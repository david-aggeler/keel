package main

import (
	"fmt"
	"strings"
	"unicode"
)

// This file holds the TypeScript half of the protocol drift pin: a reader for
// the declaration subset vsix/src/protocol.ts is written in. It is deliberately
// not a TypeScript parser — it understands exported interfaces, exported type
// aliases, and the type expressions those use, and it refuses anything else.
// Refusing is the point: a construct the reader cannot model must red the gate
// rather than be skipped into a false green.

type tsKind int

const (
	tsObjectKind tsKind = iota
	tsArrayKind
	tsPrimitiveKind
	tsStringLiteralKind
	tsNumberLiteralKind
	tsUnionKind
	tsRefKind
)

// tsType is one type expression from protocol.ts.
type tsType struct {
	kind    tsKind
	name    string // primitive name for tsPrimitiveKind, declaration name for tsRefKind
	literal string // literal text for tsStringLiteralKind and tsNumberLiteralKind
	members []tsMember
	elem    *tsType
	options []tsType
}

// tsMember is one property of an object type.
type tsMember struct {
	name     string
	optional bool
	typ      tsType
}

func (t tsType) member(name string) (tsMember, bool) {
	for _, member := range t.members {
		if member.name == name {
			return member, true
		}
	}
	return tsMember{}, false
}

type tsToken struct {
	kind string // ident, number, string, punct
	text string
}

// scanTypeScript turns protocol.ts into tokens, dropping comments and
// whitespace. Strings and comments are handled in one pass so an apostrophe
// inside a doc comment cannot unbalance the string state.
func scanTypeScript(source string) ([]tsToken, error) {
	var tokens []tsToken
	runes := []rune(source)
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '/' && i+1 < len(runes) && runes[i+1] == '/':
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
		case r == '/' && i+1 < len(runes) && runes[i+1] == '*':
			end := i + 2
			for end+1 < len(runes) && !(runes[end] == '*' && runes[end+1] == '/') {
				end++
			}
			if end+1 >= len(runes) {
				return nil, fmt.Errorf("unterminated block comment")
			}
			i = end + 2
		case r == '\'' || r == '"':
			quote := r
			i++
			start := i
			for i < len(runes) && runes[i] != quote {
				if runes[i] == '\\' {
					i++
				}
				i++
			}
			if i >= len(runes) {
				return nil, fmt.Errorf("unterminated string literal")
			}
			tokens = append(tokens, tsToken{kind: "string", text: string(runes[start:i])})
			i++
		case unicode.IsLetter(r) || r == '_' || r == '$':
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_' || runes[i] == '$') {
				i++
			}
			tokens = append(tokens, tsToken{kind: "ident", text: string(runes[start:i])})
		case unicode.IsDigit(r):
			start := i
			for i < len(runes) && (unicode.IsDigit(runes[i]) || runes[i] == '.') {
				i++
			}
			tokens = append(tokens, tsToken{kind: "number", text: string(runes[start:i])})
		case strings.ContainsRune("{}[]<>;:?|=,", r):
			tokens = append(tokens, tsToken{kind: "punct", text: string(r)})
			i++
		default:
			return nil, fmt.Errorf("unreadable character %q in protocol source", string(r))
		}
	}
	return tokens, nil
}

type tsParser struct {
	tokens []tsToken
	pos    int
}

// parseTypeScriptDeclarations reads every exported interface and type alias in
// protocol.ts. Any other top-level construct is an error, not a skip.
func parseTypeScriptDeclarations(source string) (map[string]tsType, error) {
	tokens, err := scanTypeScript(source)
	if err != nil {
		return nil, err
	}
	parser := &tsParser{tokens: tokens}
	declarations := map[string]tsType{}
	for !parser.done() {
		if err := parser.expectIdent("export"); err != nil {
			return nil, err
		}
		keyword, err := parser.nextIdent()
		if err != nil {
			return nil, err
		}
		name, err := parser.nextIdent()
		if err != nil {
			return nil, err
		}
		switch keyword {
		case "interface":
			declared, err := parser.parseObjectType()
			if err != nil {
				return nil, fmt.Errorf("interface %s: %w", name, err)
			}
			declarations[name] = declared
		case "type":
			if err := parser.expectPunct("="); err != nil {
				return nil, fmt.Errorf("type %s: %w", name, err)
			}
			declared, err := parser.parseType()
			if err != nil {
				return nil, fmt.Errorf("type %s: %w", name, err)
			}
			parser.acceptPunct(";")
			declarations[name] = declared
		default:
			return nil, fmt.Errorf("unsupported exported declaration %q %s: the protocol pin reads only interfaces and type aliases", keyword, name)
		}
	}
	return declarations, nil
}

func (p *tsParser) done() bool { return p.pos >= len(p.tokens) }

func (p *tsParser) peek() (tsToken, bool) {
	if p.done() {
		return tsToken{}, false
	}
	return p.tokens[p.pos], true
}

func (p *tsParser) next() (tsToken, error) {
	token, ok := p.peek()
	if !ok {
		return tsToken{}, fmt.Errorf("unexpected end of protocol source")
	}
	p.pos++
	return token, nil
}

func (p *tsParser) nextIdent() (string, error) {
	token, err := p.next()
	if err != nil {
		return "", err
	}
	if token.kind != "ident" {
		return "", fmt.Errorf("expected an identifier, found %q", token.text)
	}
	return token.text, nil
}

func (p *tsParser) expectIdent(want string) error {
	token, err := p.next()
	if err != nil {
		return err
	}
	if token.kind != "ident" || token.text != want {
		return fmt.Errorf("expected %q, found %q", want, token.text)
	}
	return nil
}

func (p *tsParser) expectPunct(want string) error {
	token, err := p.next()
	if err != nil {
		return err
	}
	if token.kind != "punct" || token.text != want {
		return fmt.Errorf("expected %q, found %q", want, token.text)
	}
	return nil
}

func (p *tsParser) acceptPunct(want string) bool {
	token, ok := p.peek()
	if !ok || token.kind != "punct" || token.text != want {
		return false
	}
	p.pos++
	return true
}

func (p *tsParser) parseObjectType() (tsType, error) {
	if err := p.expectPunct("{"); err != nil {
		return tsType{}, err
	}
	declared := tsType{kind: tsObjectKind}
	for {
		if p.acceptPunct("}") {
			return declared, nil
		}
		token, err := p.next()
		if err != nil {
			return tsType{}, err
		}
		if token.kind != "ident" && token.kind != "string" {
			return tsType{}, fmt.Errorf("expected a property name, found %q", token.text)
		}
		member := tsMember{name: token.text, optional: p.acceptPunct("?")}
		if err := p.expectPunct(":"); err != nil {
			return tsType{}, fmt.Errorf("property %s: %w", member.name, err)
		}
		memberType, err := p.parseType()
		if err != nil {
			return tsType{}, fmt.Errorf("property %s: %w", member.name, err)
		}
		member.typ = memberType
		declared.members = append(declared.members, member)
		if !p.acceptPunct(";") {
			p.acceptPunct(",")
		}
	}
}

func (p *tsParser) parseType() (tsType, error) {
	first, err := p.parsePostfixType()
	if err != nil {
		return tsType{}, err
	}
	options := []tsType{first}
	for p.acceptPunct("|") {
		next, err := p.parsePostfixType()
		if err != nil {
			return tsType{}, err
		}
		options = append(options, next)
	}
	if len(options) == 1 {
		return first, nil
	}
	return tsType{kind: tsUnionKind, options: options}, nil
}

func (p *tsParser) parsePostfixType() (tsType, error) {
	declared, err := p.parsePrimaryType()
	if err != nil {
		return tsType{}, err
	}
	for p.acceptPunct("[") {
		if err := p.expectPunct("]"); err != nil {
			return tsType{}, err
		}
		element := declared
		declared = tsType{kind: tsArrayKind, elem: &element}
	}
	return declared, nil
}

func (p *tsParser) parsePrimaryType() (tsType, error) {
	token, err := p.next()
	if err != nil {
		return tsType{}, err
	}
	switch {
	case token.kind == "punct" && token.text == "{":
		p.pos--
		return p.parseObjectType()
	case token.kind == "string":
		return tsType{kind: tsStringLiteralKind, literal: token.text}, nil
	case token.kind == "number":
		return tsType{kind: tsNumberLiteralKind, literal: token.text}, nil
	case token.kind == "ident" && token.text == "Array":
		if err := p.expectPunct("<"); err != nil {
			return tsType{}, err
		}
		element, err := p.parseType()
		if err != nil {
			return tsType{}, err
		}
		if err := p.expectPunct(">"); err != nil {
			return tsType{}, err
		}
		return tsType{kind: tsArrayKind, elem: &element}, nil
	case token.kind == "ident":
		switch token.text {
		case "string", "number", "boolean":
			return tsType{kind: tsPrimitiveKind, name: token.text}, nil
		default:
			return tsType{kind: tsRefKind, name: token.text}, nil
		}
	default:
		return tsType{}, fmt.Errorf("unreadable type expression at %q", token.text)
	}
}
