package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/scanner"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// inputError replaces a parser error with the name of the input and, when the
// parser gives one, the line where it stopped. Parser messages quote the keys
// and values around the failure. Lockfiles and manifests can hold credentials
// there, and these errors reach stdout, stderr, hook output and saved feedback.
func inputError(name, content string, err error) error {
	line := 0
	var parse toml.ParseError
	var syntax *json.SyntaxError
	var mismatch *json.UnmarshalTypeError
	var source scanner.ErrorList
	switch {
	case errors.As(err, &parse):
		line = parse.Position.Line
	case errors.As(err, &syntax):
		line = lineAt(content, syntax.Offset)
	case errors.As(err, &mismatch):
		line = lineAt(content, mismatch.Offset)
	case errors.As(err, &source) && len(source) > 0:
		line = source[0].Pos.Line
	}
	if line > 0 {
		return fmt.Errorf("%s cannot be parsed (line %d)", name, line)
	}
	return fmt.Errorf("%s cannot be parsed", name)
}

func lineAt(content string, offset int64) int {
	if offset < 0 || offset > int64(len(content)) {
		return 0
	}
	return 1 + strings.Count(content[:offset], "\n")
}

var toolKey = regexp.MustCompile(`^[A-Za-z0-9._-]+(?::[A-Za-z0-9@._/-]+)?$`)

// safeKey shows a key from an input only when it has the shape of a mise tool
// name. Whoever edits the file can put any string, such as a URL with
// credentials, where a key is expected.
func safeKey(key string) string {
	if len(key) > 80 || strings.Contains(key, "//") || !toolKey.MatchString(key) {
		return "(key withheld)"
	}
	return key
}

// safePath shows a path taken from an input only when it is a plain relative
// path.
func safePath(p string) string {
	if len(p) > 200 || validPath(p) != nil {
		return "(path withheld)"
	}
	return p
}
