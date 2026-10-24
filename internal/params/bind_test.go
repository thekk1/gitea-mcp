package params

import (
	"strings"
	"testing"
)

func TestBind_RequiredFieldsPresent(t *testing.T) {
	type args struct {
		Owner string `json:"owner" required:"true"`
		Repo  string `json:"repo" required:"true"`
	}
	var out args
	err := Bind(map[string]any{"owner": "gitea", "repo": "gitea-mcp"}, &out)
	if err != nil {
		t.Fatalf("Bind() unexpected error = %v", err)
	}
	if out.Owner != "gitea" || out.Repo != "gitea-mcp" {
		t.Errorf("Bind() = %+v, want Owner=gitea Repo=gitea-mcp", out)
	}
}

func TestBind_RequiredFieldMissing(t *testing.T) {
	type args struct {
		Owner string `json:"owner" required:"true"`
	}
	var out args
	err := Bind(map[string]any{}, &out)
	if err == nil {
		t.Fatal("Bind() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "owner") {
		t.Errorf("Bind() error = %v, want mentioning %q", err, "owner")
	}
}

func TestBind_RequiredStringFieldEmpty(t *testing.T) {
	type args struct {
		Owner string `json:"owner" required:"true"`
	}
	var out args
	err := Bind(map[string]any{"owner": ""}, &out)
	if err == nil {
		t.Fatal("Bind() expected error for empty required string, got nil")
	}
}

func TestBind_OptionalFieldDefaultsToZeroValue(t *testing.T) {
	type args struct {
		Owner     string `json:"owner" required:"true"`
		OldBranch string `json:"old_branch"`
	}
	var out args
	err := Bind(map[string]any{"owner": "gitea"}, &out)
	if err != nil {
		t.Fatalf("Bind() unexpected error = %v", err)
	}
	if out.OldBranch != "" {
		t.Errorf("Bind() OldBranch = %q, want empty", out.OldBranch)
	}
}

func TestBind_NumericConversion(t *testing.T) {
	type args struct {
		Page    int   `json:"page"`
		PerPage int64 `json:"per_page"`
	}
	var out args
	err := Bind(map[string]any{"page": float64(2), "per_page": float64(40)}, &out)
	if err != nil {
		t.Fatalf("Bind() unexpected error = %v", err)
	}
	if out.Page != 2 || out.PerPage != 40 {
		t.Errorf("Bind() = %+v, want Page=2 PerPage=40", out)
	}
}

func TestBind_Boolean(t *testing.T) {
	type args struct {
		Recursive bool `json:"recursive"`
	}
	var out args
	err := Bind(map[string]any{"recursive": true}, &out)
	if err != nil {
		t.Fatalf("Bind() unexpected error = %v", err)
	}
	if !out.Recursive {
		t.Errorf("Bind() Recursive = false, want true")
	}
}

func TestBind_Array(t *testing.T) {
	type args struct {
		Labels []string `json:"labels"`
		IDs    []int64  `json:"ids"`
	}
	var out args
	err := Bind(map[string]any{
		"labels": []any{"bug", "help wanted"},
		"ids":    []any{float64(1), float64(2)},
	}, &out)
	if err != nil {
		t.Fatalf("Bind() unexpected error = %v", err)
	}
	if len(out.Labels) != 2 || out.Labels[0] != "bug" || out.Labels[1] != "help wanted" {
		t.Errorf("Bind() Labels = %v, want [bug help wanted]", out.Labels)
	}
	if len(out.IDs) != 2 || out.IDs[0] != 1 || out.IDs[1] != 2 {
		t.Errorf("Bind() IDs = %v, want [1 2]", out.IDs)
	}
}

func TestBind_InvalidFieldType(t *testing.T) {
	type args struct {
		Page int `json:"page"`
	}
	var out args
	err := Bind(map[string]any{"page": "not-a-number"}, &out)
	if err == nil {
		t.Fatal("Bind() expected error for invalid numeric field, got nil")
	}
}

func TestBind_NonPointerRejected(t *testing.T) {
	type args struct {
		Owner string `json:"owner"`
	}
	err := Bind(map[string]any{}, args{})
	if err == nil {
		t.Fatal("Bind() expected error for non-pointer out, got nil")
	}
}

func TestBind_RequiredFieldNull(t *testing.T) {
	type args struct {
		Owner string `json:"owner" required:"true"`
	}
	var out args
	if err := Bind(map[string]any{"owner": nil}, &out); err == nil {
		t.Fatal("Bind() expected error for null required field, got nil")
	}
}

func TestBind_TypeMismatchMessage(t *testing.T) {
	type args struct {
		Owner string `json:"owner"`
		Page  int    `json:"page"`
	}
	tests := []struct {
		in   map[string]any
		want string
	}{
		{map[string]any{"owner": float64(1)}, "owner must be a string"},
		{map[string]any{"page": "x"}, "page must be a number"},
	}
	for _, tt := range tests {
		var out args
		err := Bind(tt.in, &out)
		if err == nil || err.Error() != tt.want {
			t.Errorf("Bind() error = %v, want %q", err, tt.want)
		}
	}
}
