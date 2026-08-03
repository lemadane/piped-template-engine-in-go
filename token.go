package pte

type TokenType string

const (
	TokenText         TokenType = "TEXT"
	TokenExpression   TokenType = "EXPRESSION"
	TokenIf           TokenType = "IF"
	TokenElseIf       TokenType = "ELSE_IF"
	TokenElse         TokenType = "ELSE"
	TokenEndIf        TokenType = "END_IF"
	TokenEach         TokenType = "EACH"
	TokenEndEach      TokenType = "END_EACH"
	TokenSwitch       TokenType = "SWITCH"
	TokenCase         TokenType = "CASE"
	TokenDefault      TokenType = "DEFAULT"
	TokenFallthrough  TokenType = "FALLTHROUGH"
	TokenEndSwitch    TokenType = "END_SWITCH"
	TokenInclude      TokenType = "INCLUDE"
	TokenLayout       TokenType = "LAYOUT"
	TokenSection      TokenType = "SECTION"
	TokenEndSection   TokenType = "END_SECTION"
	TokenYield        TokenType = "YIELD"
	TokenComponent    TokenType = "COMPONENT"
	TokenEndComponent TokenType = "END_COMPONENT"
	TokenSlot         TokenType = "SLOT"
	TokenEndSlot      TokenType = "END_SLOT"
	TokenComment      TokenType = "COMMENT"
	TokenModel        TokenType = "MODEL"
	TokenField        TokenType = "FIELD"
	TokenDisplay      TokenType = "DISPLAY"
	TokenEditor       TokenType = "EDITOR"
	TokenMacro        TokenType = "MACRO"
	TokenEndMacro     TokenType = "END_MACRO"
	TokenCall         TokenType = "CALL"
	TokenSeparator    TokenType = "SEPARATOR"
	TokenEndSeparator TokenType = "END_SEPARATOR"
	TokenFragment     TokenType = "FRAGMENT"
	TokenEndFragment  TokenType = "END_FRAGMENT"
	TokenMinify       TokenType = "MINIFY"
	TokenEndMinify    TokenType = "END_MINIFY"
	TokenPage         TokenType = "PAGE"
	TokenAttempt      TokenType = "ATTEMPT"
	TokenRecover      TokenType = "RECOVER"
	TokenEndAttempt   TokenType = "END_ATTEMPT"
	TokenPWA          TokenType = "PWA"
	TokenHTMX         TokenType = "HTMX"
	TokenHXAttr       TokenType = "HX_ATTR"
	TokenAlpine       TokenType = "ALPINE"
	TokenState        TokenType = "STATE"
	TokenAlpineAttr   TokenType = "ALPINE_ATTR"
	TokenFor          TokenType = "FOR"
	TokenEndFor       TokenType = "END_FOR"
	TokenContinue     TokenType = "CONTINUE"
	TokenBreak        TokenType = "BREAK"
	TokenRaw          TokenType = "RAW"
	TokenEndRaw       TokenType = "END_RAW"
)

type Token struct {
	Type     TokenType
	Value    string
	Position int
}
