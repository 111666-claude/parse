package xml

import (
	"bytes"
	"math"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2"
)

// MaxTransformLength is the maximum length in bytes of a transform attribute value that
// ParseTransform and ParseTransformAttr will accept. Longer values are rejected before
// any allocation proportional to the input is made.
const MaxTransformLength = 1 << 16 // 64 KiB

// TransformType determines the type of a single transform function.
type TransformType uint8

// TransformType values.
const (
	TranslateTransform TransformType = iota
	ScaleTransform
	RotateTransform
	SkewXTransform
	SkewYTransform
	MatrixTransform
)

// String returns the SVG name of the transform function.
func (tt TransformType) String() string {
	switch tt {
	case TranslateTransform:
		return "translate"
	case ScaleTransform:
		return "scale"
	case RotateTransform:
		return "rotate"
	case SkewXTransform:
		return "skewX"
	case SkewYTransform:
		return "skewY"
	case MatrixTransform:
		return "matrix"
	}
	return "Invalid(" + strconv.Itoa(int(tt)) + ")"
}

// Matrix is a 2D affine transformation matrix in the order used by SVG:
//
//	| A C E |
//	| B D F |
//	| 0 0 1 |
type Matrix struct {
	A, B, C, D, E, F float64
}

// Identity returns the identity matrix.
func Identity() Matrix {
	return Matrix{A: 1, D: 1}
}

// Mul returns the matrix product m*n. The result applies n first and m second, which
// corresponds to writing m before n in an SVG transform list.
func (m Matrix) Mul(n Matrix) Matrix {
	return Matrix{
		A: m.A*n.A + m.C*n.B,
		B: m.B*n.A + m.D*n.B,
		C: m.A*n.C + m.C*n.D,
		D: m.B*n.C + m.D*n.D,
		E: m.A*n.E + m.C*n.F + m.E,
		F: m.B*n.E + m.D*n.F + m.F,
	}
}

// Apply returns the coordinates of the point (x, y) after applying the matrix.
func (m Matrix) Apply(x, y float64) (float64, float64) {
	return m.A*x + m.C*y + m.E, m.B*x + m.D*y + m.F
}

func (m Matrix) isFinite() bool {
	return isFinite(m.A) && isFinite(m.B) && isFinite(m.C) && isFinite(m.D) && isFinite(m.E) && isFinite(m.F)
}

func isFinite(v float64) bool {
	return !math.IsInf(v, 0) && !math.IsNaN(v)
}

// degToRad converts degrees to radians without multiplying a large degree value by pi
// first, which would overflow prematurely for values above about 5.7e307.
const degToRad = math.Pi / 180.0

// Transform is a single transform function from an SVG transform list.
type Transform struct {
	Type   TransformType
	Args   []float64 // arguments as written, e.g. [2] for scale(2) and [2, 3] for scale(2,3)
	Matrix Matrix    // affine matrix of this operation alone
	Offset int       // byte offset of the function name in the parsed value
}

// String returns the transform function in its canonical form, eg. "translate(1,2)".
func (t Transform) String() string {
	var b strings.Builder
	b.WriteString(t.Type.String())
	b.WriteByte('(')
	for i, arg := range t.Args {
		if 0 < i {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(arg, 'g', -1, 64))
	}
	b.WriteByte(')')
	return b.String()
}

// TransformList is an ordered list of SVG transform functions and the combined matrix
// obtained by applying them in order.
type TransformList struct {
	Transforms []Transform
	Matrix     Matrix
}

// String returns the transform list in its canonical form, eg. "translate(1,2) scale(2)".
func (l TransformList) String() string {
	s := make([]string, len(l.Transforms))
	for i, t := range l.Transforms {
		s[i] = t.String()
	}
	return strings.Join(s, " ")
}

// ParseTransform parses an SVG transform-list, such as
// "translate(10,20) rotate(-30) skewX(15) matrix(1,0,0,1,0,0)".
//
// It returns the operations in source order together with their combined matrix. Errors
// are of type *parse.Error and carry the byte offset (reported as line and column) of the
// offending input.
func ParseTransform(b []byte) (TransformList, error) {
	return parseTransformList(b, 0, len(b))
}

// ParseTransformAttr parses the value of an SVG transform attribute as returned by
// Lexer.AttrVal, that is, including its surrounding quotes. Offsets in the result and in
// returned errors refer to positions within b, including the opening quote.
func ParseTransformAttr(b []byte) (TransformList, error) {
	start, end := 0, len(b)
	if 2 <= len(b) && (b[0] == '"' || b[0] == '\'') && b[len(b)-1] == b[0] {
		start, end = 1, len(b)-1
	}
	return parseTransformList(b, start, end)
}

func parseTransformList(b []byte, start, end int) (TransformList, error) {
	list := TransformList{Matrix: Identity()}
	if MaxTransformLength < end-start {
		return TransformList{}, parse.NewError(bytes.NewReader(b), start, "transform value exceeds maximum length of %d bytes", MaxTransformLength)
	}
	s := transformScanner{b: b, i: start, end: end}
	s.skipWSP()
	for !s.eof() {
		t, err := s.parseTransform()
		if err != nil {
			return TransformList{}, err
		}
		list.Transforms = append(list.Transforms, t)
		list.Matrix = list.Matrix.Mul(t.Matrix)
		if !list.Matrix.isFinite() {
			return TransformList{}, s.errorAt(t.Offset, "non-finite matrix for %q", t.Type)
		}

		wsStart := s.i
		s.skipWSP()
		hadWSP := wsStart < s.i
		if s.eof() {
			break
		} else if s.b[s.i] == ',' {
			comma := s.i
			s.i++
			s.skipWSP()
			if s.eof() {
				return TransformList{}, s.errorAt(comma, "unexpected end of transform list after ','")
			}
		} else if !hadWSP {
			return TransformList{}, s.errorAt(s.i, "expected space or comma between transform functions")
		}
	}
	return list, nil
}

type transformScanner struct {
	b   []byte // full input, used for error context
	i   int    // current byte offset in b
	end int    // end offset of the value to parse
}

func (s *transformScanner) eof() bool {
	return s.end <= s.i
}

func (s *transformScanner) skipWSP() {
	for s.i < s.end {
		switch s.b[s.i] {
		case ' ', '\t', '\n', '\r':
			s.i++
		default:
			return
		}
	}
}

func (s *transformScanner) errorAt(offset int, format string, a ...interface{}) error {
	return parse.NewError(bytes.NewReader(s.b), offset, format, a...)
}

func (s *transformScanner) parseTransform() (Transform, error) {
	nameStart := s.i
	for s.i < s.end && isLetter(s.b[s.i]) {
		s.i++
	}
	if s.i == nameStart {
		return Transform{}, s.errorAt(nameStart, "expected transform function name")
	}
	name := string(s.b[nameStart:s.i])

	var tt TransformType
	switch name {
	case "translate":
		tt = TranslateTransform
	case "scale":
		tt = ScaleTransform
	case "rotate":
		tt = RotateTransform
	case "skewX":
		tt = SkewXTransform
	case "skewY":
		tt = SkewYTransform
	case "matrix":
		tt = MatrixTransform
	default:
		return Transform{}, s.errorAt(nameStart, "unknown transform function %q", name)
	}

	s.skipWSP()
	if s.eof() || s.b[s.i] != '(' {
		return Transform{}, s.errorAt(s.i, "expected '(' after %q", name)
	}
	s.i++

	var args []float64
	maxArgs := maxArgumentCount(tt)
	s.skipWSP()
	if s.eof() {
		return Transform{}, s.errorAt(nameStart, "unterminated %q: expected ')'", name)
	} else if s.b[s.i] == ')' {
		return Transform{}, s.errorAt(nameStart, "%s expects %s, got 0", name, argumentCount(tt))
	}
	for {
		if len(args) == maxArgs {
			return Transform{}, s.errorAt(s.i, "%s expects %s, got more", name, argumentCount(tt))
		}
		argStart := s.i
		v, next, status := scanNumber(s.b, s.i, s.end)
		switch status {
		case numberRange:
			return Transform{}, s.errorAt(argStart, "non-finite number in %q", name)
		case numberInvalid:
			return Transform{}, s.errorAt(argStart, "expected number in %q", name)
		}
		args = append(args, v)
		s.i = next

		wsStart := s.i
		s.skipWSP()
		hadWSP := wsStart < s.i
		if s.eof() {
			return Transform{}, s.errorAt(nameStart, "unterminated %q: expected ')'", name)
		}
		c := s.b[s.i]
		if c == ')' {
			s.i++
			break
		} else if c == ',' {
			s.i++
			s.skipWSP()
			if s.eof() {
				return Transform{}, s.errorAt(nameStart, "unterminated %q: expected ')'", name)
			} else if s.b[s.i] == ')' || s.b[s.i] == ',' {
				return Transform{}, s.errorAt(s.i, "expected number in %q", name)
			}
		} else if !hadWSP || !isNumberStart(c) {
			return Transform{}, s.errorAt(s.i, "expected ',' or ')' in %q", name)
		}
	}

	if !validArgumentCount(tt, len(args)) {
		return Transform{}, s.errorAt(nameStart, "%s expects %s, got %d", name, argumentCount(tt), len(args))
	}
	m := transformMatrix(tt, args)
	if !m.isFinite() {
		return Transform{}, s.errorAt(nameStart, "non-finite matrix for %q", name)
	}
	return Transform{Type: tt, Args: args, Matrix: m, Offset: nameStart}, nil
}

func argumentCount(tt TransformType) string {
	switch tt {
	case TranslateTransform, ScaleTransform:
		return "1 or 2 arguments"
	case RotateTransform:
		return "1 or 3 arguments"
	case SkewXTransform, SkewYTransform:
		return "1 argument"
	case MatrixTransform:
		return "6 arguments"
	}
	return "arguments"
}

func validArgumentCount(tt TransformType, n int) bool {
	switch tt {
	case TranslateTransform, ScaleTransform:
		return n == 1 || n == 2
	case RotateTransform:
		return n == 1 || n == 3
	case SkewXTransform, SkewYTransform:
		return n == 1
	case MatrixTransform:
		return n == 6
	}
	return false
}

func maxArgumentCount(tt TransformType) int {
	switch tt {
	case TranslateTransform, ScaleTransform:
		return 2
	case RotateTransform:
		return 3
	case SkewXTransform, SkewYTransform:
		return 1
	case MatrixTransform:
		return 6
	}
	return 0
}

func transformMatrix(tt TransformType, args []float64) Matrix {
	switch tt {
	case TranslateTransform:
		ty := 0.0
		if 1 < len(args) {
			ty = args[1]
		}
		return Matrix{A: 1, D: 1, E: args[0], F: ty}
	case ScaleTransform:
		sy := args[0]
		if 1 < len(args) {
			sy = args[1]
		}
		return Matrix{A: args[0], D: sy}
	case RotateTransform:
		rad := args[0] * degToRad
		cos, sin := math.Cos(rad), math.Sin(rad)
		m := Matrix{A: cos, B: sin, C: -sin, D: cos}
		if 2 < len(args) {
			cx, cy := args[1], args[2]
			m.E = cx - cos*cx + sin*cy
			m.F = cy - sin*cx - cos*cy
		}
		return m
	case SkewXTransform:
		return Matrix{A: 1, C: math.Tan(args[0] * degToRad), D: 1}
	case SkewYTransform:
		return Matrix{A: 1, B: math.Tan(args[0] * degToRad), D: 1}
	case MatrixTransform:
		return Matrix{A: args[0], B: args[1], C: args[2], D: args[3], E: args[4], F: args[5]}
	}
	return Identity()
}

const (
	numberInvalid = iota
	numberOK
	numberRange
)

// scanNumber scans an SVG number at b[i:end] and returns its value, the offset just past
// it, and its status. Scientific notation, leading signs, and a leading or trailing dot
// are accepted. A value that overflows float64 is reported as numberRange.
func scanNumber(b []byte, i, end int) (float64, int, int) {
	start := i
	if i < end && (b[i] == '+' || b[i] == '-') {
		i++
	}
	digits := 0
	for i < end && isDigit(b[i]) {
		i++
		digits++
	}
	if i < end && b[i] == '.' {
		i++
		for i < end && isDigit(b[i]) {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0, start, numberInvalid
	}
	if i < end && (b[i] == 'e' || b[i] == 'E') {
		j := i + 1
		if j < end && (b[j] == '+' || b[j] == '-') {
			j++
		}
		expDigits := 0
		for j < end && isDigit(b[j]) {
			j++
			expDigits++
		}
		if expDigits == 0 {
			return 0, start, numberInvalid
		}
		i = j
	}

	v, err := strconv.ParseFloat(string(b[start:i]), 64)
	if err != nil {
		if math.IsInf(v, 0) {
			return v, i, numberRange
		}
		return 0, start, numberInvalid
	}
	return v, i, numberOK
}

func isDigit(c byte) bool {
	return '0' <= c && c <= '9'
}

func isLetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func isNumberStart(c byte) bool {
	return isDigit(c) || c == '+' || c == '-' || c == '.'
}
