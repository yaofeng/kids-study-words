package mcpserver

import "testing"

func TestMCPCreateAndRenameNotebook(t *testing.T) {
	env := newTestEnv(t)
	created := decodeOut[NotebookOutput](t, env.call(t, "create_notebook", map[string]any{"name": "动物"}))
	if created.Notebook.Name != "动物" || created.Notebook.IsDefault {
		t.Fatalf("created=%+v", created)
	}
	renamed := decodeOut[NotebookOutput](t, env.call(t, "rename_notebook", map[string]any{
		"notebook_id": created.Notebook.ID, "name": "宠物",
	}))
	if renamed.Notebook.Name != "宠物" {
		t.Fatalf("renamed=%+v", renamed)
	}
	res := env.call(t, "create_notebook", map[string]any{"name": "宠物"})
	if !res.IsError {
		t.Fatal("duplicate should be a tool error")
	}
}
