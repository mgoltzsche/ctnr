package model

import (
	"github.com/mgoltzsche/cntnr/log"
	"regexp"
	"strings"
)

var substitutionRegex = regexp.MustCompile("\\$[a-zA-Z0-9_]+|\\$\\{[a-zA-Z0-9_]+(:?-.*?)?\\}")

type Substitution func(v string) string

func NewSubstitution(kv map[string]string, warn log.Logger) Substitution {
	subExpr := func(v string) string {
		return substituteExpression(v, kv, warn)
	}
	return func(v string) string {
		return substitutionRegex.ReplaceAllStringFunc(v, subExpr)
	}
}

func substituteExpression(v string, kv map[string]string, warn log.Logger) string {
	varName := ""
	defaultVal := ""
	hasDefault := false
	if v[1] == '{' {
		// ${VAR:-default} syntax
		exprEndPos := len(v) - 1
		minusPos := strings.Index(v, "-")
		if minusPos == -1 {
			varName = v[2:exprEndPos]
		} else {
			varEndPos := minusPos
			colonPos := strings.Index(v, ":")
			if colonPos == minusPos-1 {
				varEndPos = colonPos
			}
			varName = v[2:varEndPos]
			defaultVal = v[minusPos+1 : exprEndPos]
			hasDefault = true
		}
	} else {
		// $VAR syntax
		varName = v[1:]
	}
	if s, ok := kv[varName]; ok {
		return s
	} else {
		if !hasDefault {
			warn.Printf("Warn: %s env var is not set. Defaulting to blank string.", varName)
		}
		return defaultVal
	}
}
