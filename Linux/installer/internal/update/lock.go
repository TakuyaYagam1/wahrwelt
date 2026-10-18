package update

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/TakuyaYagam1/wahrwelt/Linux/installer/internal/apply"
)

type githubSource struct {
	owner, repo, ref, dir string
	query                 url.Values
}

func parseSource(raw string) (githubSource, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "github" || parsed.Opaque == "" {
		return githubSource{}, errors.New("automatic updates require a literal GitHub Wahrwelt source")
	}
	parts := strings.Split(parsed.Opaque, "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] == "" {
		return githubSource{}, errors.New("invalid GitHub Wahrwelt source")
	}
	source := githubSource{owner: parts[0], repo: parts[1], query: parsed.Query()}
	if len(parts) == 3 {
		source.ref = parts[2]
	}
	source.dir = source.query.Get("dir")
	return source, nil
}

func sameRepository(left, right githubSource) bool {
	if left.owner != right.owner {
		return false
	}
	if left.repo == right.repo {
		return true
	}
	return left.owner == "TakuyaYagam1" &&
		(left.repo == "MySetup" && right.repo == "wahrwelt" || left.repo == "wahrwelt" && right.repo == "MySetup")
}

func pinnedURL(desiredURL, resolvedURL, revision string) (string, error) {
	desired, err := parseSource(desiredURL)
	if err != nil {
		return "", err
	}
	resolved, err := parseSource(resolvedURL)
	if err != nil {
		return "", err
	}
	if !sameRepository(desired, resolved) || resolved.ref != revision {
		return "", errors.New("resolved Wahrwelt source does not match the configured repository and revision")
	}
	// A migration can select another preset in the same pinned repository.
	// Preserve metadata's content hash while applying that new directory.
	resolved.query.Set("dir", desired.dir)
	resolved.query.Del("ref")
	resolved.query.Del("rev")
	return "github:" + resolved.owner + "/" + resolved.repo + "/" + revision + "?" + resolved.query.Encode(), nil
}

func verifyLock(data []byte, opts Options, flake string) error {
	var lock struct {
		Root  string `json:"root"`
		Nodes map[string]struct {
			Inputs map[string]json.RawMessage `json:"inputs"`
			Locked struct {
				Type  string `json:"type"`
				Owner string `json:"owner"`
				Repo  string `json:"repo"`
				Rev   string `json:"rev"`
				Dir   string `json:"dir"`
			} `json:"locked"`
		} `json:"nodes"`
	}
	if err := jsonv2.Unmarshal(data, &lock); err != nil {
		return fmt.Errorf("read prepared flake lock: %w", err)
	}
	root, ok := lock.Nodes[lock.Root]
	if !ok {
		return errors.New("prepared lock has no root node")
	}
	var name string
	if err := jsonv2.Unmarshal(root.Inputs["wahrwelt"], &name); err != nil {
		return errors.New("prepared lock must directly select its Wahrwelt input")
	}
	node, ok := lock.Nodes[name]
	if !ok || node.Locked.Type != "github" || node.Locked.Rev != opts.TargetRevision {
		return errors.New("prepared Wahrwelt lock does not match the updater revision")
	}
	target, err := parseSource(opts.TargetURL)
	if err != nil {
		return err
	}
	if !sameRepository(target, githubSource{owner: node.Locked.Owner, repo: node.Locked.Repo}) {
		return errors.New("prepared Wahrwelt lock points to another repository")
	}
	desiredURL, err := apply.CurrentWahrweltURL(flake)
	if err != nil {
		return err
	}
	desired, err := parseSource(desiredURL)
	if err != nil {
		return err
	}
	if node.Locked.Dir != desired.dir {
		return errors.New("prepared Wahrwelt lock selects another configuration directory")
	}
	return nil
}
