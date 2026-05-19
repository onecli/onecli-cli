package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/v1/projects" {
			t.Errorf("path = %q, want /v1/projects", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode([]Project{
			{ID: "p1", Name: "Alpha", Slug: "alpha"},
		})
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_test", "/v1")
	projects, err := client.ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].ID != "p1" {
		t.Errorf("got %+v", projects)
	}
}

func TestGetProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/v1/projects/p1" {
			t.Errorf("path = %q, want /v1/projects/p1", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Project{ID: "p1", Name: "Alpha", Slug: "alpha"})
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_test", "/v1")
	project, err := client.GetProject(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if project.ID != "p1" || project.Name != "Alpha" {
		t.Errorf("got %+v", project)
	}
}

func TestCreateProject(t *testing.T) {
	var gotBody CreateProjectInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/projects" {
			t.Errorf("path = %q, want /v1/projects", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Project{ID: "p2", Name: gotBody.Name, Slug: "beta"})
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_test", "/v1")
	project, err := client.CreateProject(context.Background(), CreateProjectInput{Name: "Beta"})
	if err != nil {
		t.Fatal(err)
	}
	if project.ID != "p2" {
		t.Errorf("got %+v", project)
	}
	if gotBody.Name != "Beta" {
		t.Errorf("request body name = %q, want Beta", gotBody.Name)
	}
}

func TestUpdateProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %q, want PATCH", r.Method)
		}
		if r.URL.Path != "/v1/projects/p1" {
			t.Errorf("path = %q, want /v1/projects/p1", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Project{ID: "p1", Name: "Renamed", Slug: "alpha"})
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_test", "/v1")
	name := "Renamed"
	project, err := client.UpdateProject(context.Background(), "p1", UpdateProjectInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if project.Name != "Renamed" {
		t.Errorf("got name %q, want Renamed", project.Name)
	}
}

func TestDeleteProject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		if r.URL.Path != "/v1/projects/p1" {
			t.Errorf("path = %q, want /v1/projects/p1", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_test", "/v1")
	err := client.DeleteProject(context.Background(), "p1")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestListProjectsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server error"}`))
	}))
	defer srv.Close()

	client := newWithPrefix(srv.URL, "oc_test", "/v1")
	_, err := client.ListProjects(context.Background())
	if err == nil {
		t.Error("expected error")
	}
}
