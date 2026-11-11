package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestCreateFolderInsideParentAPI(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	parent, err := f.svc.CreateFolder(t.Context(), f.owner, "Parent", "parent")
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"name": "Nested", "parent_id": parent.ID, "op_id": "nested"}
	rec := f.do(t, http.MethodPost, "/v1/folders", body, f.session())
	requireStatus(t, rec, http.StatusOK)
	created := decodeBody[service.Folder](t, rec)
	if created.ParentID != parent.ID {
		t.Fatalf("created folder at root instead of inside parent: %+v", created)
	}
	rec = f.do(t, http.MethodPost, "/v1/folders", body, f.session())
	requireStatus(t, rec, http.StatusOK)
	if again := decodeBody[service.Folder](t, rec); again != created {
		t.Fatalf("non-idempotent create: %+v vs %+v", again, created)
	}
	rec = f.do(t, http.MethodPost, "/v1/folders", map[string]any{"name": "Orphan", "parent_id": parent.ID + 1000, "op_id": "bad-parent"}, f.session())
	if rec.Code < 400 {
		t.Fatal("accepted missing parent")
	}
	folders, err := f.svc.Folders(t.Context(), f.owner)
	if err != nil || len(folders) != 2 || folders[1].ParentID != parent.ID {
		t.Fatalf("folder parent was not persisted or failed create left an orphan: %+v %v", folders, err)
	}
}

func TestFolderMoveAPI(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	parent, err := f.svc.CreateFolder(t.Context(), f.owner, "Parent", "parent")
	if err != nil {
		t.Fatal(err)
	}
	child, err := f.svc.CreateFolder(t.Context(), f.owner, "Child", "child")
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/v1/folders/%d", child.ID)
	rec := f.do(t, http.MethodPatch, path, map[string]any{"parent_id": parent.ID, "op_id": "move"}, f.session())
	requireStatus(t, rec, http.StatusOK)
	var moved service.Folder
	if err := json.Unmarshal(rec.Body.Bytes(), &moved); err != nil {
		t.Fatal(err)
	}
	if moved.ParentID != parent.ID || moved.Name != child.Name {
		t.Fatal(moved)
	}
	rec = f.do(t, http.MethodPatch, path, map[string]any{"name": "Renamed", "op_id": "rename"}, f.session())
	requireStatus(t, rec, http.StatusOK)
	if err := json.Unmarshal(rec.Body.Bytes(), &moved); err != nil {
		t.Fatal(err)
	}
	if moved.ParentID != parent.ID || moved.Name != "Renamed" {
		t.Fatal(moved)
	}
	rec = f.do(t, http.MethodPatch, fmt.Sprintf("/v1/folders/%d", parent.ID), map[string]any{"parent_id": child.ID, "op_id": "cycle"}, f.session())
	if rec.Code < 400 {
		t.Fatal("cycle accepted", rec.Body.String())
	}
	rec = f.do(t, http.MethodPatch, path, map[string]any{"parent_id": 0, "op_id": "root"}, f.session())
	requireStatus(t, rec, http.StatusOK)
	if err := json.Unmarshal(rec.Body.Bytes(), &moved); err != nil {
		t.Fatal(err)
	}
	if moved.ParentID != 0 {
		t.Fatal(moved)
	}
	rec = f.do(t, http.MethodPatch, path, map[string]any{"op_id": "empty"}, f.session())
	if rec.Code < 400 {
		t.Fatal("empty patch accepted")
	}
}
