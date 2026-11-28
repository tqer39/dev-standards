package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/scanner"
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
		// Offset は不正なバイトの次を指す。そのバイトが改行でも、改行のある行を示す。
		// 入力が途中で終わった場合は不正なバイトがなく、Offset が末尾そのものを指す。
		offset := syntax.Offset - 1
		if syntax.Error() == "unexpected end of JSON input" {
			offset = syntax.Offset
		}
		line = lineAt(content, offset)
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
