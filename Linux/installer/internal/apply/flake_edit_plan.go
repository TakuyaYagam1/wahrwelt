package apply

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func inputName(path string) string {
	parts := strings.Split(path, ".")
	if len(parts) >= 3 && parts[0] == "inputs" {
		return parts[1]
	}
	return ""
}

func retainReferencedInputs(doc *flakeDocument, changes map[string]*string) {
	// Keeping one input can keep another through its follows. Iterate until all
	// remaining references are outside the ranges that would be deleted.
	for {
		removed := deletedFlakeSpans(doc, changes)
		retained := false
		for path, value := range changes {
			name := inputName(path)
			if value != nil || name == "" || path != "inputs."+name+".url" {
				continue
			}
			if !inputHasUserFields(doc, name, changes) && !inputReferenced(doc, name, removed) {
				continue
			}
			for key := range changes {
				if strings.HasPrefix(key, "inputs."+name+".") {
					delete(changes, key)
				}
			}
			retained = true
		}
		if !retained {
			return
		}
	}
}

func inputHasUserFields(doc *flakeDocument, name string, changes map[string]*string) bool {
	if doc.hasEmptyInputSet("inputs." + name) {
		return true
	}
	for path := range doc.leaves {
		if !strings.HasPrefix(path, "inputs."+name+".") {
			continue
		}
		value, changing := changes[path]
		if !changing || value != nil {
			return true
		}
	}
	return false
}

func inputReferenced(doc *flakeDocument, name string, removed []flakeEdit) bool {
	for index, token := range doc.tokens {
		value, _ := strconv.Unquote(token.text)
		interpolates := strings.Contains(token.text, "${") && (strings.Contains(token.text, name) || strings.Contains(token.text, "inputs"))
		follows := value == name || strings.HasPrefix(value, name+"/")
		if token.text != name && !follows && !interpolates && !unknownInputsReference(doc, index) && !unknownInputFollow(doc, token) {
			continue
		}
		deleted := false
		for _, span := range removed {
			if token.start >= span.start && token.end <= span.end {
				deleted = true
				break
			}
		}
		if !deleted {
			return true
		}
	}
	return false
}

func unknownInputFollow(doc *flakeDocument, token flakeToken) bool {
	for path, binding := range doc.leaves {
		if !strings.HasPrefix(path, "inputs.") || !strings.HasSuffix(path, ".follows") || binding.valueStart != token.start {
			continue
		}
		value, err := strconv.Unquote(doc.text[binding.valueStart:binding.valueEnd])
		return err != nil || strings.Contains(value, "${")
	}
	return false
}

func unknownInputsReference(doc *flakeDocument, index int) bool {
	tokens := doc.tokens
	if tokens[index].text != "inputs" {
		return false
	}
	if index+1 < len(tokens) && tokens[index+1].text == "@" {
		return false
	}
	for _, binding := range doc.nodes {
		if tokens[index].start >= binding.start && tokens[index].end <= binding.valueStart {
			return false
		}
	}
	if index >= 2 && index+1 < len(tokens) && tokens[index-2].text == "inherit" && tokens[index-1].text == "(" && tokens[index+1].text == ")" {
		return false
	}
	if index+2 < len(tokens) && tokens[index+1].text == "." {
		_, static := flakeAttrName(tokens[index+2].text)
		return !static
	}
	return true
}

func deletedFlakeSpans(doc *flakeDocument, changes map[string]*string) []flakeEdit {
	var spans []flakeEdit
	for path, binding := range doc.nodes {
		if path == "inputs" || !allFlakeLeavesDeleted(doc, path, changes) {
			continue
		}
		spans = append(spans, flakeEdit{start: binding.start, end: binding.end})
	}
	for path, value := range changes {
		if value == nil && doc.leaves[path] != nil {
			binding := doc.leaves[path]
			spans = append(spans, flakeEdit{start: binding.start, end: binding.end})
		}
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end > spans[j].end
		}
		return spans[i].start < spans[j].start
	})
	result := make([]flakeEdit, 0, len(spans))
	for _, span := range spans {
		if len(result) == 0 || span.start >= result[len(result)-1].end {
			result = append(result, span)
		}
	}
	return result
}

func allFlakeLeavesDeleted(doc *flakeDocument, path string, changes map[string]*string) bool {
	if doc.hasEmptyInputSet(path) {
		return false
	}
	found := false
	for leaf := range doc.leaves {
		if leaf != path && !strings.HasPrefix(leaf, path+".") {
			continue
		}
		value, changing := changes[leaf]
		if !changing || value != nil {
			return false
		}
		found = true
	}
	for leaf, value := range changes {
		if value != nil && strings.HasPrefix(leaf, path+".") {
			return false
		}
	}
	return found
}

func (doc *flakeDocument) hasEmptyInputSet(path string) bool {
	for name, set := range doc.sets {
		if (name == path || strings.HasPrefix(name, path+".")) && len(set.bindings) == 0 {
			return true
		}
	}
	return false
}

func (doc *flakeDocument) planEdits(changes map[string]*string) ([]flakeEdit, []string, error) {
	edits := deletedFlakeSpans(doc, changes)
	additions := map[string]map[string]string{}
	labels := make([]string, 0, len(changes))
	for path, value := range changes {
		if value == nil {
			labels = append(labels, "remove "+path)
			continue
		}
		if binding := doc.leaves[path]; binding != nil {
			edits = append(edits, flakeEdit{binding.valueStart, binding.valueEnd, *value})
			labels = append(labels, "update "+path)
			continue
		}
		parent := doc.insertionParent(path)
		if parent == "" && strings.HasPrefix(path, "host.") {
			return nil, nil, fmt.Errorf("cannot add %s without a literal mkWahrweltHost argument set", path)
		}
		if additions[parent] == nil {
			additions[parent] = map[string]string{}
		}
		relative := strings.TrimPrefix(path, parent+".")
		if parent == "" {
			relative = path
		}
		additions[parent][relative] = *value
		labels = append(labels, "add "+path)
	}
	parents := make([]string, 0, len(additions))
	for parent := range additions {
		parents = append(parents, parent)
	}
	sort.Strings(parents)
	for _, parent := range parents {
		set := doc.sets[parent]
		indent := flakeLineIndent(doc.text, set.close)
		content := "\n" + renderFlakeAdditions(additions[parent], indent+"  ") + indent
		edits = append(edits, flakeEdit{set.close, set.close, content})
	}
	sort.Strings(labels)
	return edits, labels, nil
}

func (doc *flakeDocument) insertionParent(path string) string {
	for strings.Contains(path, ".") {
		path = path[:strings.LastIndexByte(path, '.')]
		if doc.sets[path] != nil {
			return path
		}
	}
	return ""
}

func flakeLineIndent(text string, at int) string {
	start := strings.LastIndexByte(text[:at], '\n') + 1
	end := start
	for end < at && (text[end] == ' ' || text[end] == '\t') {
		end++
	}
	return text[start:end]
}

func renderFlakeAdditions(fields map[string]string, indent string) string {
	names := map[string]map[string]string{}
	for path, value := range fields {
		name, rest, hasRest := strings.Cut(path, ".")
		if names[name] == nil {
			names[name] = map[string]string{}
		}
		if !hasRest {
			rest = ""
		}
		names[name][rest] = value
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	var result strings.Builder
	for _, name := range ordered {
		if value, leaf := names[name][""]; leaf {
			result.WriteString(indent + name + " = " + value + ";\n")
		} else {
			result.WriteString(indent + name + " = {\n")
			result.WriteString(renderFlakeAdditions(names[name], indent+"  "))
			result.WriteString(indent + "};\n")
		}
	}
	return result.String()
}
