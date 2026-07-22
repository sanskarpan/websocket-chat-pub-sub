package sanitization

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "Hello World", "Hello World"},
		{"with html tags", "Hello <b>World</b>", "Hello &lt;b&gt;World&lt;/b&gt;"},
		{"with script", "Hello <script>alert('xss')</script>", "Hello &lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeHTML(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStripHTML(t *testing.T) {
	input := "Hello <b>World</b>"
	got := StripHTML(input)
	assert.Equal(t, "Hello World", got)
}

func TestSanitizeMessage(t *testing.T) {
	input := "  <script>alert('xss')</script>Test  "
	got := SanitizeMessage(input)
	assert.NotContains(t, got, "<script>")
	assert.Contains(t, got, "Test")
}

func TestMaskEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
		want  string
	}{
		{"normal email", "john@example.com", "j**n@example.com"},
		{"short username", "ab@example.com", "**@example.com"},
		{"invalid email", "invalid", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskEmail(tt.email)
			assert.Equal(t, tt.want, got)
		})
	}
}
