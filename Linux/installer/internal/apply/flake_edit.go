package apply

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The editor only interprets static attribute paths and literal input sets.
// Other Nix expressions are kept as spans, never evaluated or reformatted.
type flakeToken struct {
	text       string
	start, end int
}

type flakeBinding struct {
	path                 string
	start, end           int
	valueStart, valueEnd int
	first, last          int
}

type flakeSet struct {
	open, close int
	bindings    []*flakeBinding
	opaque      bool
	inherited   []string
}

type flakeDocument struct {
	text   string
	tokens []flakeToken
	leaves map[string]*flakeBinding
	sets   map[string]*flakeSet
	nodes  map[string]*flakeBinding
}

type flakeEdit struct {
	start, end int
	text       string
}

func scanFlake(text string) ([]flakeToken, error) {
	if len(text) > 2*1024*1024 {
		return nil, fmt.Errorf("flake.nix exceeds the 2 MiB editing limit")
	}
	tokens := make([]flakeToken, 0, len(text)/8)
	for i := 0; i < len(text); {
		if strings.ContainsRune(" \t\r\n", rune(text[i])) {
			i++
			continue
		}
		if text[i] == '#' {
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				break
			}
			i += end + 1
			continue
		}
		if strings.HasPrefix(text[i:], "/*") {
			end := nixBlockCommentEnd(text, i)
			if end == len(text) && !strings.HasSuffix(text, "*/") {
				return nil, fmt.Errorf("unterminated comment in flake.nix")
			}
			i = end
			continue
		}
		end, err := flakeTokenEnd(text, i)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, flakeToken{text[i:end], i, end})
		i = end
	}
	return tokens, nil
}

func flakeTokenEnd(text string, start int) (int, error) {
	if text[start] == '"' || strings.HasPrefix(text[start:], "''") {
		return flakeStringEnd(text, start, 0)
	}
	if word := nixURITokenPattern.FindString(text[start:]); word != "" {
		return start + len(word), nil
	}
	if word := nixPathTokenPattern.FindString(text[start:]); word != "" {
		return start + len(word), nil
	}
	if word := nixIdentifierPattern.FindString(text[start:]); word != "" {
		return start + len(word), nil
	}
	return start + 1, nil
}

func flakeStringEnd(text string, start, depth int) (int, error) {
	if depth > 128 {
		return 0, fmt.Errorf("flake.nix string nesting exceeds the editing limit")
	}
	indented := strings.HasPrefix(text[start:], "''")
	delimiter := "\""
	if indented {
		delimiter = "''"
	}
	for i := start + len(delimiter); i < len(text); {
		switch {
		case !indented && text[i] == '\\':
			i += 2
		case indented && (strings.HasPrefix(text[i:], "'''") || strings.HasPrefix(text[i:], "''$")):
			i += 3
		case indented && strings.HasPrefix(text[i:], "''\\"):
			i += 4
		case strings.HasPrefix(text[i:], delimiter):
			return i + len(delimiter), nil
		case strings.HasPrefix(text[i:], "${"):
			end, err := flakeInterpolationEnd(text, i+2, depth+1)
			if err != nil {
				return 0, err
			}
			i = end
		default:
			i++
		}
	}
	return 0, fmt.Errorf("unterminated string in flake.nix")
}

func flakeInterpolationEnd(text string, start, depth int) (int, error) {
	braces := 1
	for i := start; i < len(text); {
		switch {
		case text[i] == '"' || strings.HasPrefix(text[i:], "''"):
			end, err := flakeStringEnd(text, i, depth)
			if err != nil {
				return 0, err
			}
			i = end
		case strings.HasPrefix(text[i:], "/*"):
			i = nixBlockCommentEnd(text, i)
		case text[i] == '#':
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				return 0, fmt.Errorf("unterminated interpolation in flake.nix")
			}
			i += end + 1
		case text[i] == '{':
			braces++
			i++
		case text[i] == '}':
			braces--
			i++
			if braces == 0 {
				return i, nil
			}
		default:
			i++
		}
	}
	return 0, fmt.Errorf("unterminated interpolation in flake.nix")
}

func parseFlake(text string) (*flakeDocument, error) {
	tokens, err := scanFlake(text)
	if err != nil {
		return nil, err
	}
	doc := &flakeDocument{text: text, tokens: tokens, leaves: map[string]*flakeBinding{}, sets: map[string]*flakeSet{}, nodes: map[string]*flakeBinding{}}
	root, end, err := doc.parseSet(0, 0)
	if err != nil {
		return nil, err
	}
	if end != len(tokens) {
		return nil, fmt.Errorf("flake.nix must be a literal top-level attribute set")
	}
	if root.opaque {
		return nil, fmt.Errorf("inherited top-level flake fields require manual reconciliation")
	}
	doc.sets[""] = root
	for _, binding := range root.bindings {
		switch {
		case binding.path == "inputs" || strings.HasPrefix(binding.path, "inputs."):
			if err := doc.flattenInputs(binding, binding.path, 0); err != nil {
				return nil, err
			}
		case binding.path == "description":
			doc.leaves["description"] = binding
		case binding.path == "outputs":
			if err := doc.readHost(binding); err != nil {
				return nil, err
			}
		}
	}
	return doc, nil
}

func (doc *flakeDocument) parseSet(first, depth int) (*flakeSet, int, error) {
	if depth > 128 || first >= len(doc.tokens) || doc.tokens[first].text != "{" {
		return nil, 0, fmt.Errorf("managed flake attributes must use literal sets")
	}
	set := &flakeSet{open: doc.tokens[first].start}
	paths := make(map[string]bool)
	for i := first + 1; i < len(doc.tokens); {
		if doc.tokens[i].text == "}" {
			set.close = doc.tokens[i].start
			return set, i + 1, nil
		}
		if doc.tokens[i].text == "inherit" {
			end, err := doc.expressionEnd(i)
			if err != nil {
				return nil, 0, err
			}
			set.opaque = true
			level := 0
			for _, token := range doc.tokens[i+1 : end] {
				switch token.text {
				case "(":
					level++
				case ")":
					level--
				default:
					if level == 0 {
						set.inherited = append(set.inherited, token.text)
					}
				}
			}
			i = end + 1
			continue
		}
		binding, next, err := doc.parseBinding(i)
		if err != nil {
			return nil, 0, err
		}
		if paths[binding.path] {
			return nil, 0, fmt.Errorf("duplicate flake attribute %s", binding.path)
		}
		paths[binding.path] = true
		set.bindings = append(set.bindings, binding)
		i = next
	}
	return nil, 0, fmt.Errorf("unterminated attribute set in flake.nix")
}

func (doc *flakeDocument) parseBinding(first int) (*flakeBinding, int, error) {
	parts := []string{}
	i := first
	for {
		if i >= len(doc.tokens) {
			return nil, 0, fmt.Errorf("unfinished attribute path in flake.nix")
		}
		part, ok := flakeAttrName(doc.tokens[i].text)
		if !ok {
			return nil, 0, fmt.Errorf("unsupported attribute path near byte %d in flake.nix", doc.tokens[i].start)
		}
		parts = append(parts, part)
		i++
		if i < len(doc.tokens) && doc.tokens[i].text == "." {
			i++
			continue
		}
		break
	}
	if i >= len(doc.tokens) || doc.tokens[i].text != "=" {
		return nil, 0, fmt.Errorf("expected an assignment for %s", strings.Join(parts, "."))
	}
	end, err := doc.expressionEnd(i + 1)
	if err != nil {
		return nil, 0, err
	}
	if end == i+1 {
		return nil, 0, fmt.Errorf("empty value for %s", strings.Join(parts, "."))
	}
	return &flakeBinding{strings.Join(parts, "."), doc.tokens[first].start, doc.tokens[end].end,
		doc.tokens[i+1].start, doc.tokens[end-1].end, i + 1, end}, end + 1, nil
}

func flakeAttrName(token string) (string, bool) {
	if nixIdentifierPattern.FindString(token) == token {
		return token, true
	}
	value, err := strconv.Unquote(token)
	return value, err == nil && value != "" && !strings.Contains(token, "${") && nixIdentifierPattern.FindString(value) == value
}

func (doc *flakeDocument) expressionEnd(first int) (int, error) {
	stack := []string{}
	lets := 0
	sequences := 0
	for i := first; i < len(doc.tokens); i++ {
		token := doc.tokens[i].text
		if len(stack) == 0 {
			switch token {
			case "let":
				lets++
			case "in":
				lets--
			case "with", "assert":
				sequences++
			case ";":
				if sequences > 0 {
					sequences--
				} else if lets == 0 {
					return i, nil
				}
			}
		}
		switch token {
		case "{", "[", "(":
			stack = append(stack, token)
		case "}", "]", ")":
			if len(stack) == 0 || !flakeDelimitersMatch(stack[len(stack)-1], token) {
				return 0, fmt.Errorf("unbalanced expression in flake.nix near byte %d", doc.tokens[i].start)
			}
			stack = stack[:len(stack)-1]
		}
	}
	return 0, fmt.Errorf("unfinished expression in flake.nix")
}

func flakeDelimitersMatch(open, closing string) bool {
	return (open == "{" && closing == "}") || (open == "[" && closing == "]") || (open == "(" && closing == ")")
}

func (doc *flakeDocument) flattenInputs(binding *flakeBinding, path string, depth int) error {
	if _, exists := doc.nodes[path]; exists {
		return fmt.Errorf("duplicate flake attribute %s", path)
	}
	doc.nodes[path] = binding
	if doc.tokens[binding.first].text != "{" {
		if path == "inputs" || len(strings.Split(path, ".")) == 2 {
			return fmt.Errorf("%s must be a literal attribute set for automatic updates", path)
		}
		doc.leaves[path] = binding
		return nil
	}
	set, next, err := doc.parseSet(binding.first, depth+1)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if next != binding.last || set.opaque {
		return fmt.Errorf("%s uses unsupported inherited or computed attributes", path)
	}
	doc.sets[path] = set
	for _, child := range set.bindings {
		if err := doc.flattenInputs(child, path+"."+child.path, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (doc *flakeDocument) readHost(outputs *flakeBinding) error {
	if err := doc.readHostBindings(outputs); err != nil {
		return err
	}
	for i := outputs.first; i+4 < outputs.last; i++ {
		if !doc.isHostCall(i) {
			continue
		}
		if _, exists := doc.sets["host"]; exists {
			return fmt.Errorf("multiple Wahrwelt hosts in one flake require manual reconciliation")
		}
		set, _, err := doc.parseSet(i+5, 0)
		if err != nil {
			return fmt.Errorf("mkWahrweltHost arguments: %w", err)
		}
		doc.sets["host"] = set
		for _, name := range set.inherited {
			if name != "system" && name != "hostname" {
				return fmt.Errorf("inherited mkWahrweltHost argument %s requires manual reconciliation", name)
			}
		}
		for _, binding := range set.bindings {
			doc.leaves["host."+binding.path] = binding
		}
	}
	return nil
}

func (doc *flakeDocument) isHostCall(index int) bool {
	tokens := doc.tokens[index : index+5]
	return (tokens[0].text == "wahrwelt" || tokens[0].text == "mysetup") &&
		tokens[1].text == "." && tokens[2].text == "lib" && tokens[3].text == "." &&
		(tokens[4].text == "mkWahrweltHost" || tokens[4].text == "mkMySetupHost")
}

func (doc *flakeDocument) readHostBindings(outputs *flakeBinding) error {
	depth := 0
	for i := outputs.first; i+1 < outputs.last; i++ {
		switch doc.tokens[i].text {
		case "{", "[", "(":
			depth++
		case "}", "]", ")":
			depth--
		case ":":
			if depth == 0 && doc.tokens[i+1].text == "let" {
				return doc.readLetBindings(i+2, outputs.last)
			}
		}
	}
	return nil
}

func (doc *flakeDocument) readLetBindings(first, last int) error {
	for i := first; i < last && doc.tokens[i].text != "in"; {
		if doc.tokens[i].text == "inherit" {
			end, err := doc.expressionEnd(i)
			if err != nil {
				return err
			}
			i = end + 1
			continue
		}
		binding, next, err := doc.parseBinding(i)
		if err != nil {
			return fmt.Errorf("outputs let bindings: %w", err)
		}
		if binding.path == "hostname" || binding.path == "system" {
			key := "binding." + binding.path
			if doc.leaves[key] != nil {
				return fmt.Errorf("duplicate outputs let binding %s", binding.path)
			}
			doc.leaves[key] = binding
		}
		i = next
	}
	return nil
}

func (doc *flakeDocument) values() map[string]string {
	values := make(map[string]string, len(doc.leaves))
	for path, binding := range doc.leaves {
		values[path] = doc.text[binding.valueStart:binding.valueEnd]
	}
	return values
}

func flakeValueEqual(left, right string) bool {
	a, errA := scanFlake(left)
	b, errB := scanFlake(right)
	if errA != nil || errB != nil || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].text != b[i].text {
			return false
		}
	}
	return true
}

func applyFlakeEdits(text string, edits []flakeEdit) (string, error) {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var result strings.Builder
	previous := 0
	for _, edit := range edits {
		if edit.start < previous || edit.end < edit.start || edit.end > len(text) {
			return "", fmt.Errorf("overlapping edits in flake.nix; no files were changed")
		}
		result.WriteString(text[previous:edit.start])
		result.WriteString(edit.text)
		previous = edit.end
	}
	result.WriteString(text[previous:])
	return result.String(), nil
}
