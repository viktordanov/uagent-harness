package queue_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"example.com/jobq/queue"
)

// The agentbench check.

func TestHiddenJobType(t *testing.T) {
	var j queue.Job = queue.NewJob("echo", "x")
	if j.Kind != "echo" || j.Payload != "x" || j.State != queue.Pending || j.ID != "" {
		t.Fatalf("NewJob = %+v", j)
	}
	if err := j.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestHiddenDeadState(t *testing.T) {
	dead, err := queue.ParseState("dead")
	if err != nil {
		t.Fatalf("ParseState(dead): %v", err)
	}
	if string(dead) != "dead" || !dead.Valid() || !dead.Terminal() {
		t.Fatalf("dead: %q valid %v terminal %v", dead, dead.Valid(), dead.Terminal())
	}
	found := false
	for _, st := range queue.States() {
		if st == dead {
			found = true
		}
	}
	if !found {
		t.Fatalf("States() = %v has no dead", queue.States())
	}
	for _, name := range []string{"pending", "running", "done"} {
		if _, err := queue.ParseState(name); err != nil {
			t.Errorf("ParseState(%s): %v", name, err)
		}
	}
}

// TestHiddenNoTaskLeft parses every Go file of the module: the type Task and
// the function NewTask must be gone.
func TestHiddenNoTaskLeft(t *testing.T) {
	root := ".."
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name == "NewTask" {
					t.Errorf("%s: func NewTask is still declared", fset.Position(d.Pos()))
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == "Task" {
						t.Errorf("%s: type Task is still declared", fset.Position(ts.Pos()))
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
