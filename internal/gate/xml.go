package gate

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type identity struct{ class, name string }

type frameKind uint8

const (
	wrapperFrame frameKind = iota
	suiteFrame
	caseFrame
	opaqueFrame
)

type frame struct {
	kind           frameKind
	name           xml.Name
	suite, test    int
	counts         Counts
	declared       [4]int64
	present        [4]bool
	failure, error bool
	skipped        bool
}

var counterNames = [4]string{"tests", "failures", "errors", "skipped"}

// The supported declaration grammar is XML 1.0, UTF-8, optional standalone.
// It is intentionally narrower than a general XML declaration processor.
var declaration = regexp.MustCompile(`^version[ \t\r\n]*=[ \t\r\n]*(?:"1\.0"|'1\.0')(?:[ \t\r\n]+encoding[ \t\r\n]*=[ \t\r\n]*(?:"(?i:UTF-8)"|'(?i:UTF-8)'))?(?:[ \t\r\n]+standalone[ \t\r\n]*=[ \t\r\n]*(?:"(?:yes|no)"|'(?:yes|no)'))?[ \t\r\n]*$`)

type xmlState struct {
	run           *run
	file          int
	stack         []frame
	rootSeen      bool
	declarationOK bool
	firstToken    bool
	suites, cases int
	totals        Counts
}

var errBudget = errors.New("input budget exceeded")

type budgetReader struct {
	run        *run
	input      io.Reader
	bytes      int64
	code       string
	readFailed bool
}

func (b *budgetReader) Read(p []byte) (int, error) {
	if b.code != "" {
		return 0, errBudget
	}
	remaining := min(b.run.limits.MaxFileBytes-b.bytes, b.run.limits.MaxTotalBytes-b.run.bytes)
	if remaining < 0 {
		if b.run.bytes > b.run.limits.MaxTotalBytes {
			b.code = "xml_limit_total_bytes"
		} else {
			b.code = "xml_limit_bytes"
		}
		return 0, errBudget
	}
	if int64(len(p)) > remaining {
		p = p[:int(remaining)+1]
	}
	n, err := b.input.Read(p)
	if err != nil && err != io.EOF {
		b.readFailed = true
	}
	b.bytes += int64(n)
	b.run.bytes += int64(n)
	if b.bytes > b.run.limits.MaxFileBytes {
		b.code = "xml_limit_bytes"
	} else if b.run.bytes > b.run.limits.MaxTotalBytes {
		b.code = "xml_limit_total_bytes"
	}
	if b.code != "" {
		return n, errBudget
	}
	return n, err
}

func (r *run) parseXML(input io.Reader, file int) Counts {
	bounded := &budgetReader{run: r, input: input}
	decoder := xml.NewDecoder(bounded)
	decoder.Strict = true
	decoder.Entity = nil
	decoder.CharsetReader = nil
	state := xmlState{run: r, file: file, declarationOK: true, firstToken: true}
	for {
		token, err := decoder.Token()
		if bounded.code != "" {
			state.issue(bounded.code)
			break
		}
		if err != nil {
			if err == io.EOF {
				if !state.rootSeen || len(state.stack) != 0 {
					state.issue("xml_invalid")
				}
			} else if bounded.readFailed {
				state.issue("report_read_error")
			} else if strings.Contains(err.Error(), "CharsetReader") {
				state.issue("xml_encoding")
			} else {
				state.issue("xml_invalid")
			}
			break
		}
		valid := true
		switch t := token.(type) {
		case xml.StartElement:
			state.declarationOK = false
			valid = state.start(t)
		case xml.EndElement:
			valid = state.end(t)
		case xml.Directive:
			state.issue("xml_directive")
			valid = false
		case xml.ProcInst:
			if t.Target != "xml" || !state.declarationOK || !declaration.Match(t.Inst) {
				state.issue("xml_structure")
				valid = false
			}
			state.declarationOK = false
		case xml.CharData:
			data := []byte(t)
			if state.firstToken && bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
				data = data[3:]
				// A leading UTF-8 BOM may precede an XML declaration.
				if len(data) == 0 {
					state.firstToken = false
					continue
				}
			}
			if !utf8.Valid(data) {
				state.issue("xml_encoding")
				valid = false
			} else if len(state.stack) == 0 || state.stack[len(state.stack)-1].kind != opaqueFrame {
				if !xmlWhitespace(data) {
					state.issue("xml_structure")
					valid = false
				}
			}
			state.declarationOK = false
		case xml.Comment:
			state.declarationOK = false
		}
		state.firstToken = false
		if !valid {
			break
		}
	}
	return state.totals
}

func xmlWhitespace(data []byte) bool {
	for _, ch := range data {
		if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' {
			return false
		}
	}
	return true
}

func (s *xmlState) issue(code string) {
	suite, test := 0, 0
	if len(s.stack) > 0 {
		suite = s.stack[len(s.stack)-1].suite
		test = s.stack[len(s.stack)-1].test
	}
	s.run.diagnostic(code, s.file, suite, test)
}

func (s *xmlState) start(t xml.StartElement) bool {
	s.run.elements++
	if s.run.elements > s.run.limits.MaxElements {
		s.issue("xml_limit_elements")
		return false
	}
	if len(s.stack) >= s.run.limits.MaxDepth {
		s.issue("xml_limit_depth")
		return false
	}
	if len(t.Attr) > s.run.limits.MaxAttributes {
		s.issue("xml_limit_attributes")
		return false
	}
	attrs := make(map[xml.Name]struct{}, len(t.Attr))
	for _, attr := range t.Attr {
		if _, exists := attrs[attr.Name]; exists {
			s.issue("xml_duplicate_attribute")
			return false
		}
		attrs[attr.Name] = struct{}{}
	}
	next := frame{name: t.Name}
	if len(s.stack) > 0 {
		parent := &s.stack[len(s.stack)-1]
		next.suite, next.test = parent.suite, parent.test
		if parent.kind == opaqueFrame {
			next.kind = opaqueFrame
			s.stack = append(s.stack, next)
			return true
		}
	}
	if t.Name.Space != "" || strings.Contains(t.Name.Local, ":") {
		s.issue("xml_namespace")
		return false
	}
	for _, attr := range t.Attr {
		// encoding/xml does not enforce all namespace constraints. An empty
		// prefix binding could otherwise erase a prefixed element's Space.
		if attr.Name.Space == "xmlns" && attr.Value == "" {
			s.issue("xml_namespace")
			return false
		}
		if attr.Name.Space != "" && semanticAttribute(attr.Name.Local) {
			s.issue("xml_namespace")
			return false
		}
	}
	if len(s.stack) == 0 {
		if s.rootSeen || (t.Name.Local != "testsuite" && t.Name.Local != "testsuites") {
			s.issue("xml_structure")
			return false
		}
		s.rootSeen = true
		if t.Name.Local == "testsuite" {
			next.kind = suiteFrame
		} else {
			next.kind = wrapperFrame
		}
	} else {
		parent := &s.stack[len(s.stack)-1]
		switch parent.kind {
		case wrapperFrame:
			if t.Name.Local != "testsuite" {
				s.issue("xml_structure")
				return false
			}
			next.kind = suiteFrame
		case suiteFrame:
			switch t.Name.Local {
			case "testsuite":
				next.kind = suiteFrame
			case "testcase":
				next.kind = caseFrame
			case "properties", "system-out", "system-err":
				next.kind = opaqueFrame
			default:
				s.issue("xml_structure")
				return false
			}
		case caseFrame:
			next.kind = opaqueFrame
			switch t.Name.Local {
			case "failure":
				parent.failure = true
			case "error":
				parent.error = true
			case "skipped":
				parent.skipped = true
			case "properties", "system-out", "system-err":
			case "flakyFailure", "flakyError", "rerunFailure", "rerunError":
				s.issue("xml_retry_unsupported")
				return false
			case "testcase", "testsuite", "testsuites":
				s.issue("xml_structure")
				return false
			default:
				s.issue("xml_result_unknown")
				return false
			}
		}
	}
	if next.kind == suiteFrame {
		s.suites++
		next.suite, next.test = s.suites, 0
	}
	if next.kind == suiteFrame || next.kind == wrapperFrame {
		for _, attr := range []string{"status", "result", "outcome", "disabled"} {
			if _, exists := attribute(t, attr); exists {
				s.issue("xml_result_unknown")
				return false
			}
		}
		for i, counter := range counterNames {
			value, exists := attribute(t, counter)
			if !exists {
				if i == 0 && next.kind == suiteFrame {
					s.run.diagnostic("count_missing_tests", s.file, next.suite, 0)
				}
				continue
			}
			parsed, ok := decimal(value)
			if !ok {
				s.run.diagnostic("count_invalid_"+counter, s.file, next.suite, 0)
				continue
			}
			next.declared[i], next.present[i] = parsed, true
		}
	}
	if next.kind == caseFrame {
		s.run.cases++
		s.cases++
		next.test = s.cases
		if s.run.cases > s.run.limits.MaxCases {
			s.issue("xml_limit_cases")
			return false
		}
		name, _ := attribute(t, "name")
		class, _ := attribute(t, "classname")
		if strings.TrimSpace(name) == "" {
			s.run.diagnostic("xml_name_invalid", s.file, next.suite, next.test)
			return false
		}
		if len(name) > s.run.limits.MaxIdentityBytes || len(class) > s.run.limits.MaxIdentityBytes {
			s.run.diagnostic("xml_limit_identity", s.file, next.suite, next.test)
			return false
		}
		key := identity{class, name}
		if _, exists := s.run.identities[key]; exists {
			s.run.diagnostic("duplicate_case", s.file, next.suite, next.test)
		}
		s.run.identities[key] = struct{}{}
		for _, attr := range []string{"status", "result", "outcome", "disabled"} {
			if _, exists := attribute(t, attr); exists {
				s.run.diagnostic("xml_result_unknown", s.file, next.suite, next.test)
				return false
			}
		}
	}
	s.stack = append(s.stack, next)
	return true
}

func attribute(t xml.StartElement, name string) (string, bool) {
	for _, attr := range t.Attr {
		if attr.Name.Space == "" && attr.Name.Local == name {
			return attr.Value, true
		}
	}
	return "", false
}

func semanticAttribute(name string) bool {
	switch name {
	case "tests", "failures", "errors", "skipped", "name", "classname", "status", "result", "outcome", "disabled":
		return true
	default:
		return false
	}
}

func decimal(value string) (int64, bool) {
	if value == "" || len(value) > 19 {
		return 0, false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, false
		}
	}
	number, err := strconv.ParseInt(value, 10, 64)
	return number, err == nil
}

func (s *xmlState) end(t xml.EndElement) bool {
	if len(s.stack) == 0 || s.stack[len(s.stack)-1].name != t.Name {
		s.issue("xml_structure")
		return false
	}
	closed := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	if closed.kind == opaqueFrame {
		return true
	}
	if closed.kind == caseFrame {
		closed.counts.Cases = 1
		if closed.failure {
			closed.counts.Failures = 1
			s.run.diagnostic("test_failure", s.file, closed.suite, closed.test)
		}
		if closed.error {
			closed.counts.Errors = 1
			s.run.diagnostic("test_error", s.file, closed.suite, closed.test)
		}
		if closed.skipped && (closed.failure || closed.error) {
			s.run.diagnostic("conflicting_outcomes", s.file, closed.suite, closed.test)
		}
		if closed.skipped && !closed.failure && !closed.error {
			closed.counts.Skipped = 1
		} else {
			closed.counts.NonSkipped = 1
		}
		if !closed.failure && !closed.error && !closed.skipped {
			closed.counts.Passed = 1
		}
	} else {
		observed := [4]int{closed.counts.Cases, closed.counts.Failures, closed.counts.Errors, closed.counts.Skipped}
		for i, counter := range counterNames {
			if closed.present[i] && closed.declared[i] != int64(observed[i]) {
				actual, expected := int64(observed[i]), closed.declared[i]
				s.run.addDiagnostic(Diagnostic{Code: "count_mismatch_" + counter, File: s.file, Suite: closed.suite, Actual: &actual, Expected: &expected}, 1)
			}
		}
	}
	if len(s.stack) == 0 {
		s.totals.add(closed.counts)
	} else {
		s.stack[len(s.stack)-1].counts.add(closed.counts)
	}
	return true
}
