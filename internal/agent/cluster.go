// Package agent — cluster.go derives a stable identity for "task type" so the
// affinity store can learn which kinds of work the user wants planned versus
// just executed.
//
// The cluster identity is intentionally lossy: better to have ~30 stable
// clusters across thousands of unique inputs than 3000 unique strings the
// learner can never accumulate enough samples for.
//
// cluster_id = sha1(verb + "|" + object_class)[:12]
//
// where verb is the first matching action verb and object_class is the
// strongest contextual signal we can extract cheaply.
package agent

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

// TaskCluster is the cheap, deterministic identity for a task type.
type TaskCluster struct {
	ID          string // 12-char hex of sha1(verb|object)
	Verb        string
	ObjectClass string
}

// String returns "verb/object" — useful for logging and user-facing narration.
func (c TaskCluster) String() string {
	if c.Verb == "" && c.ObjectClass == "" {
		return "chat/freeform"
	}
	v := c.Verb
	if v == "" {
		v = "chat"
	}
	o := c.ObjectClass
	if o == "" {
		o = "freeform"
	}
	return v + "/" + o
}

// clusterVerbs is the master list. Order matters — earlier verbs win on tie.
// Includes both the hardActionVerbs from router.go and softer verbs that still
// shape task behaviour (summarize, explain, find, etc.).
var clusterVerbs = []string{
	// Hard verbs (touch real systems / produce artifacts)
	"deploy", "install", "uninstall", "refactor", "migrate", "configure",
	"set up", "scaffold", "bootstrap", "provision", "rollback", "publish",
	"merge", "rebase", "commit", "push", "pull", "checkout",
	"build", "create", "make", "write", "implement", "develop",
	"fix", "debug", "generate",
	// Soft verbs (read / explain / ideate)
	"summarize", "summarise", "explain", "describe", "compare",
	"find", "search", "lookup", "look up", "check", "review",
	"brainstorm", "suggest", "recommend", "list", "show",
	"read", "open", "fetch", "load",
}

// objectClassRules maps regex/keyword patterns to object-class labels.
// First match wins. The freeform fallback ensures every input gets a class.
var objectClassRules = []struct {
	match func(string) bool
	class string
}{
	{func(s string) bool { return youtubePattern.MatchString(s) }, "youtube"},
	{func(s string) bool { return strings.Contains(s, "github.com") }, "github"},
	{func(s string) bool { return strings.Contains(s, "stackoverflow.com") }, "stackoverflow"},
	{func(s string) bool { return strings.HasSuffix(strings.Fields(s)[len(strings.Fields(s))-1], ".pdf") }, "pdf"},
	{func(s string) bool { return urlPattern.MatchString(s) }, "web"},
	{fileExtensionMatch([]string{".go", ".py", ".js", ".ts", ".rs", ".java", ".c", ".cpp", ".rb"}), "code-file"},
	{fileExtensionMatch([]string{".yaml", ".yml", ".toml", ".json", ".env", ".ini"}), "config-file"},
	{fileExtensionMatch([]string{".md", ".txt", ".rst"}), "doc-file"},
	{func(s string) bool {
		return strings.Contains(s, "error") || strings.Contains(s, "stack trace") ||
			strings.Contains(s, "traceback") || strings.Contains(s, "panic") ||
			strings.Count(s, "\n") > 2
	}, "error-text"},
	{func(s string) bool { return strings.Contains(s, "?") }, "question"},
}

// fileExtensionMatch returns a predicate that matches inputs containing any
// listed extension as a token boundary.
func fileExtensionMatch(exts []string) func(string) bool {
	return func(s string) bool {
		for _, ext := range exts {
			// Match ".go " or ".go" at end, not ".godzilla"
			if strings.Contains(s, ext+" ") || strings.HasSuffix(s, ext) {
				return true
			}
		}
		return false
	}
}

// ClusterFor returns the TaskCluster for an arbitrary user input.
//
// Inputs that match no verb collapse to verb="chat"; inputs that match no
// object class collapse to object="freeform". Both fallbacks together produce
// the cluster `chat/freeform` which absorbs all greetings and small talk.
func ClusterFor(input string) TaskCluster {
	lower := strings.ToLower(strings.TrimSpace(input))
	if lower == "" {
		return TaskCluster{ID: clusterID("chat", "freeform"), Verb: "chat", ObjectClass: "freeform"}
	}

	verb := "chat"
	for _, v := range clusterVerbs {
		if strings.Contains(lower, v) {
			verb = v
			break
		}
	}

	object := "freeform"
	for _, rule := range objectClassRules {
		if rule.match(lower) {
			object = rule.class
			break
		}
	}

	return TaskCluster{ID: clusterID(verb, object), Verb: verb, ObjectClass: object}
}

// clusterID returns the 12-char hex identifier for a (verb, object) pair.
func clusterID(verb, object string) string {
	sum := sha1.Sum([]byte(verb + "|" + object))
	return hex.EncodeToString(sum[:])[:12]
}
