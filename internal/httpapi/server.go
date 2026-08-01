package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/trly/pkgdepot/internal/repository"
)

const maxUploadSize = 2 << 30

type Server struct {
	repositories *repository.Service
	tokenHash    [sha256.Size]byte
}

func New(repositories *repository.Service, token string) http.Handler {
	server := &Server{repositories: repositories, tokenHash: sha256.Sum256([]byte(token))}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("GET /repos/{repository}/{architecture}/{filename}", server.download)
	mux.Handle("GET /api/v1/repositories/{repository}/{architecture}/packages", server.authenticate(http.HandlerFunc(server.list)))
	mux.Handle("POST /api/v1/repositories/{repository}/{architecture}/packages", server.authenticate(http.HandlerFunc(server.publish)))
	mux.Handle("DELETE /api/v1/repositories/{repository}/{architecture}/packages/{package}", server.authenticate(http.HandlerFunc(server.remove)))
	return mux
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")
		provided, found := strings.CutPrefix(authorization, "Bearer ")
		if !found || provided == "" || sha256.Sum256([]byte(provided)) != s.tokenHash {
			writeError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	directory, err := s.repositories.RepositoryDirectory(r.PathValue("repository"), r.PathValue("architecture"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filename := r.PathValue("filename")
	if filename == "" || filename != filepath.Base(filename) || strings.Contains(filename, `\`) {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	file, err := os.Open(filepath.Join(directory, filename))
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "open repository file")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, filename, info.ModTime(), file)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	packages, err := s.repositories.List(r.PathValue("repository"), r.PathValue("architecture"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, packages)
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload")
		return
	}
	defer r.MultipartForm.RemoveAll()
	packageFile, packageHeader, err := r.FormFile("package")
	if err != nil {
		writeError(w, http.StatusBadRequest, "package form field is required")
		return
	}
	defer packageFile.Close()

	var signatureFile multipartFile
	signature, _, err := r.FormFile("signature")
	if err == nil {
		signatureFile = signature
		defer signature.Close()
	} else if !errors.Is(err, http.ErrMissingFile) {
		writeError(w, http.StatusBadRequest, "invalid signature form field")
		return
	}

	pkg, err := s.repositories.Publish(r.Context(), r.PathValue("repository"), r.PathValue("architecture"), packageHeader.Filename, packageFile, signatureFile)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, pkg)
}

type multipartFile interface {
	Read([]byte) (int, error)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	err := s.repositories.Remove(r.Context(), r.PathValue("repository"), r.PathValue("architecture"), r.PathValue("package"))
	if errors.Is(err, repository.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf("%s", message)})
}
