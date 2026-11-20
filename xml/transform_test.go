package xml

import (
	"math"
	"strings"
	"testing"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/test"
)

func matrixFloats(m Matrix) []float64 {
	return []float64{m.A, m.B, m.C, m.D, m.E, m.F}
}

// TestParseTransformNested verifies a transform list typical of nested SVG groups against
// values computed independently with standard SVG composition rules (see the comment in
// svgcalc.py used to derive them). The outer group's matrix and the inner group's matrix
// are checked separately, then composed, and finally applied to a point.
func TestParseTransformNested(t *testing.T) {
	outer := `translate(48.5,-12.25) rotate(-30,10,10) scale(1.5,.75)`
	inner := `skewX(15) matrix(1,.2,-.2,1,5,0)`

	gotOuter, err := ParseTransform([]byte(outer))
	test.Error(t, err)
	test.Floats(t, matrixFloats(gotOuter.Matrix), []float64{
		1.299038105676658, -0.7499999999999999, 0.37499999999999994,
		0.649519052838329, 44.83974596215561, -5.910254037844387,
	})
	test.That(t, len(gotOuter.Transforms) == 3)
	test.T(t, gotOuter.Transforms[0].Type, TranslateTransform)
	test.T(t, gotOuter.Transforms[1].Type, RotateTransform)
	test.T(t, gotOuter.Transforms[2].Type, ScaleTransform)
	test.That(t, gotOuter.Transforms[0].Offset == 0)
	test.That(t, gotOuter.Transforms[1].Offset == len(`translate(48.5,-12.25) `))
	test.That(t, gotOuter.Transforms[2].Offset == len(`translate(48.5,-12.25) rotate(-30,10,10) `))

	gotInner, err := ParseTransform([]byte(inner))
	test.Error(t, err)
	test.Floats(t, matrixFloats(gotInner.Matrix), []float64{
		1.0535898384862246, 0.2, 0.06794919243112268, 1, 5, 0,
	})

	// Composing group matrices: outer applied to everything inside it.
	combined := gotOuter.Matrix.Mul(gotInner.Matrix)
	test.Floats(t, matrixFloats(combined), []float64{
		1.4436533479473213, -0.6602885682970026, 0.4632685902179843,
		0.598557158514987, 51.3349364905389, -9.660254037844387,
	})

	// Point (3,-4) in the inner group's coordinate system.
	x, y := gotOuter.Matrix.Apply(3, -4)
	test.Floats(t, []float64{x, y}, []float64{47.236860279185585, -10.758330249197703})
	x, y = combined.Apply(3, -4)
	test.Floats(t, []float64{x, y}, []float64{53.81282217350893, -14.035348376795344})
}

func TestParseTransformMatrixMatchesSequentialApplication(t *testing.T) {
	// SVG composes a transform list as T1*T2*...*Tn, so a point is transformed by the
	// last function first. For translate(100 200) rotate(90) scale(2) we get
	// p -> scale(p) -> rotate -> translate, which must equal applying the combined matrix.
	got, err := ParseTransform([]byte(`translate(100 200) rotate(90) scale(2)`))
	test.Error(t, err)

	x, y := 1.0, 0.0
	for i := len(got.Transforms) - 1; 0 <= i; i-- {
		x, y = got.Transforms[i].Matrix.Apply(x, y)
	}
	mx, my := got.Matrix.Apply(1, 0)
	test.FloatDiff(t, mx, x, 1e-12)
	test.FloatDiff(t, my, y, 1e-12)
	// (1,0) -> scale 2 -> (2,0) -> rotate 90 -> (0,2) -> translate -> (100,202)
	test.FloatDiff(t, mx, 100, 1e-12)
	test.FloatDiff(t, my, 202, 1e-12)

	// Operations are returned in source order.
	test.T(t, got.Transforms[0].Type, TranslateTransform)
	test.T(t, got.Transforms[1].Type, RotateTransform)
	test.T(t, got.Transforms[2].Type, ScaleTransform)
}

func TestParseTransformSyntax(t *testing.T) {
	var tests = []struct {
		input  string
		matrix []float64
		args   []float64 // args of the first transform, when checked
	}{
		{`translate(1)`, []float64{1, 0, 0, 1, 1, 0}, []float64{1}},
		{`translate(1,2)`, []float64{1, 0, 0, 1, 1, 2}, []float64{1, 2}},
		{`translate( 1 , 2 )`, []float64{1, 0, 0, 1, 1, 2}, []float64{1, 2}},
		{`translate(1
2)`, []float64{1, 0, 0, 1, 1, 2}, []float64{1, 2}},
		{"translate(1\t2)", []float64{1, 0, 0, 1, 1, 2}, []float64{1, 2}},
		{`scale(2)`, []float64{2, 0, 0, 2, 0, 0}, []float64{2}},
		{`scale(2,-3)`, []float64{2, 0, 0, -3, 0, 0}, []float64{2, -3}},
		{`skewX(45)`, []float64{1, 0, 1, 1, 0, 0}, []float64{45}},
		{`skewY(45)`, []float64{1, 1, 0, 1, 0, 0}, []float64{45}},
		{`matrix(1,2,3,4,5,6)`, []float64{1, 2, 3, 4, 5, 6}, []float64{1, 2, 3, 4, 5, 6}},
		{`matrix(1e0,2E0,3e-0,4E+0,.5,6.)`, []float64{1, 2, 3, 4, 0.5, 6}, []float64{1, 2, 3, 4, 0.5, 6}},
		{`translate(1e2 -2.5E-1)`, []float64{1, 0, 0, 1, 100, -0.25}, []float64{100, -0.25}},
		{`translate(-0.5e+1)`, []float64{1, 0, 0, 1, -5, 0}, []float64{-5}},
		{`rotate(1e308)`, []float64{-0.2392127917195601, 0.9709671674561063, -0.9709671674561063, -0.2392127917195601, 0, 0}, nil}, // large angle stays finite
		{``, []float64{1, 0, 0, 1, 0, 0}, nil},
		{`  `, []float64{1, 0, 0, 1, 0, 0}, nil},
		{`translate(1),rotate(90)`, []float64{0, 1, -1, 0, 1, 0}, []float64{1}},
		{`translate(1) , rotate(90)`, []float64{0, 1, -1, 0, 1, 0}, []float64{1}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseTransform([]byte(tt.input))
			test.Error(t, err)
			test.Floats(t, matrixFloats(got.Matrix), tt.matrix)
			if tt.args != nil {
				test.That(t, 0 < len(got.Transforms))
				test.Floats(t, got.Transforms[0].Args, tt.args)
			}
		})
	}
}

func TestParseTransformRotateCenter(t *testing.T) {
	// Rotating 90 degrees around (10,20) must map (10,20) to itself and (11,20) to
	// (9,21), i.e. just like translate(cx,cy) rotate(90) translate(-cx,-cy).
	got, err := ParseTransform([]byte(`rotate(90,10,20)`))
	test.Error(t, err)
	test.Floats(t, matrixFloats(got.Matrix), []float64{
		0, 1, -1, 0, 30, 10,
	})
	x, y := got.Matrix.Apply(10, 20)
	test.FloatDiff(t, x, 10, 1e-12)
	test.FloatDiff(t, y, 20, 1e-12)
	x, y = got.Matrix.Apply(11, 20)
	test.FloatDiff(t, x, 10, 1e-12)
	test.FloatDiff(t, y, 21, 1e-12)

	// Equivalent explicit form must give the same matrix.
	equiv, err := ParseTransform([]byte(`translate(10 20) rotate(90) translate(-10 -20)`))
	test.Error(t, err)
	test.Floats(t, matrixFloats(equiv.Matrix), matrixFloats(got.Matrix))
}

func TestParseTransformAttr(t *testing.T) {
	got, err := ParseTransformAttr([]byte(`"translate(1,2) rotate(90)"`))
	test.Error(t, err)
	test.Floats(t, matrixFloats(got.Matrix), []float64{0, 1, -1, 0, 1, 2})
	// Offsets include the opening quote.
	test.T(t, got.Transforms[0].Offset, 1)
	test.T(t, got.Transforms[1].Offset, len(`"translate(1,2) `))

	got, err = ParseTransformAttr([]byte(`'scale(2)'`))
	test.Error(t, err)
	test.Floats(t, matrixFloats(got.Matrix), []float64{2, 0, 0, 2, 0, 0})

	// Unquoted and mismatched quotes: parsed as-is from the first byte.
	got, err = ParseTransformAttr([]byte(`translate(3)`))
	test.Error(t, err)
	test.Floats(t, matrixFloats(got.Matrix), []float64{1, 0, 0, 1, 3, 0})
}

func TestParseTransformRoundTrip(t *testing.T) {
	inputs := []string{
		`translate(48.5,-12.25) rotate(-30,10,10) scale(1.5,.75) skewX(15) matrix(1,.2,-.2,1,5,0)`,
		`rotate(90,10,20) translate(1e3 -2.5E-2)`,
		`skewY(-12.5) scale(-1)`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			list, err := ParseTransform([]byte(input))
			test.Error(t, err)
			reparsed, err := ParseTransform([]byte(list.String()))
			test.Error(t, err)
			test.String(t, reparse(list.Transforms), list.String())
			test.Floats(t, matrixFloats(reparsed.Matrix), matrixFloats(list.Matrix))
		})
	}
}

func reparse(ts []Transform) string {
	return TransformList{Transforms: ts}.String()
}

func TestParseTransformErrors(t *testing.T) {
	var tests = []struct {
		input string
		col   int // column of the reported error, 1-based
	}{
		// argument counts
		{`translate()`, 1},
		{`translate(1,2,3)`, 15},
		{`scale(1,2,3)`, 11},
		{`rotate()`, 1},
		{`rotate(1,2)`, 1},
		{`rotate(1,2,3,4)`, 14},
		{`skewX()`, 1},
		{`skewX(1,2)`, 9},
		{`skewY(1,2)`, 9},
		{`matrix(1,2,3,4,5)`, 1},
		{`matrix(1,2,3,4,5,6,7)`, 20},

		// numbers
		{`translate(1e999)`, 11},                                // non-finite
		{`translate(-1e999)`, 11},                               // non-finite, negative
		{`matrix(1e308,0,0,1,0,0) matrix(1e308,0,0,1,0,0)`, 25}, // product overflows at the second matrix
		{`translate(1e308) translate(1e308)`, 18},               // translation composition overflows
		{`translate(+)`, 11},
		{`translate(.)`, 11},
		{`translate(1e)`, 11},
		{`translate(NaN)`, 11},
		{`translate(Infinity)`, 11},
		{`translate(1,,2)`, 13},
		{`translate(1,)`, 13},

		// structure
		{`foo(1)`, 1},
		{`Translate(1)`, 1},
		{`translate`, 10},   // missing '('
		{`translate 1`, 11}, // missing '(' after whitespace
		{`translate(1`, 1},  // unterminated
		{`translate((1))`, 11},
		{`translate(1))`, 13},
		{`translate(1)rotate(2)`, 13}, // missing separator
		{`translate(1),`, 13},         // trailing comma
		{`translate(1) ,`, 14},
		{`scale(2) # comment`, 10},

		// very long input
		{`scale(1) ` + strings.Repeat(`translate(1) `, MaxTransformLength/8), 1},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParseTransform([]byte(tt.input))
			test.That(t, err != nil, "expected error")
			perr, ok := err.(*parse.Error)
			if !ok {
				test.Fail(t, "expected *parse.Error, got", err)
			}
			_, col, _ := perr.Position()
			test.T(t, col, tt.col)
		})
	}
}

func TestParseTransformErrorOffsetAttr(t *testing.T) {
	// The error offset must shift with the opening quote so it points into the attribute.
	_, err := ParseTransformAttr([]byte(`"translate(1,2"`))
	test.That(t, err != nil)
	perr := err.(*parse.Error)
	_, col, _ := perr.Position()
	test.T(t, col, 2) // points at the opening quote, start of the unterminated function

	_, err = ParseTransform([]byte(`translate(1,2`))
	test.That(t, err != nil)
	perr = err.(*parse.Error)
	_, col, _ = perr.Position()
	test.T(t, col, 1)
}

// ensure the non-finite check catches NaN even if a platform's trig yields one
func TestMatrixIsFinite(t *testing.T) {
	test.That(t, !Identity().isFinite() == false)
	test.That(t, !Matrix{A: math.NaN(), D: 1}.isFinite())
	test.That(t, !Matrix{A: math.Inf(1), D: 1}.isFinite())
}

// TestParseTransformFromLexer feeds the value the XML lexer hands back for an attribute
// into the transform parser, which is how an SVG importer would use it.
func TestParseTransformFromLexer(t *testing.T) {
	const src = `<g transform="translate(48.5,-12.25) rotate(-30,10,10) scale(1.5,.75)">`
	l := NewLexer(parse.NewInputString(src))
	_, data := l.Next()
	test.Bytes(t, data, []byte("<g"))
	_, data = l.Next()
	test.Bytes(t, l.Text(), []byte("transform"))
	test.Bytes(t, l.AttrVal(), []byte(`"translate(48.5,-12.25) rotate(-30,10,10) scale(1.5,.75)"`))

	list, err := ParseTransformAttr(l.AttrVal())
	test.Error(t, err)
	test.Floats(t, matrixFloats(list.Matrix), []float64{
		1.299038105676658, -0.7499999999999999, 0.37499999999999994,
		0.649519052838329, 44.83974596215561, -5.910254037844387,
	})
	// Offsets refer to the attribute value bytes, quote included.
	test.T(t, list.Transforms[0].Offset, 1)
	test.T(t, string(l.AttrVal()[list.Transforms[1].Offset:list.Transforms[1].Offset+6]), "rotate")
}
