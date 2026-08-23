package httpapi

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/trly/pkgdepot/internal/alpm"
	"github.com/trly/pkgdepot/internal/api"
	"github.com/trly/pkgdepot/internal/auth"
	"github.com/trly/pkgdepot/internal/cimd"
	"github.com/trly/pkgdepot/internal/repository"
)

const DefaultMaxUploadSize = 500 << 20

//go:embed web
var webFiles embed.FS

var webTemplates = template.Must(template.ParseFS(webFiles, "web/*.html"))

type indexPage struct {
	AppName      string
	Repositories []repository.Repository
}

type packagesPage struct {
	AppName       string
	Repository    string
	RepositoryURL string
	Query         string
	Packages      []repositoryPackageView
}

type packagePage struct {
	AppName    string
	Repository string
	Package    repositoryPackageView
}

type repositoryPackageView struct {
	Name        string
	Description string
	DetailsURL  string
	Variants    []packageVariantView
}

type packageVariantView struct {
	alpm.Package
	TargetArchitecture string
	FormattedSize      string
	DetailsURL         string
	DownloadURL        string
	SignatureURL       string
}

type Server struct {
	appName       string
	repositories  *repository.Service
	canonicalURL  string
	maxUploadSize int64
	resourceAuth  *auth.ResourceServer
}

type Options struct {
	AppName       string
	MaxUploadSize int64
	ResourceAuth  *auth.ResourceServer
}

func New(repositories *repository.Service, canonicalURL string, options ...Options) http.Handler {
	maxUploadSize := int64(DefaultMaxUploadSize)
	if len(options) > 0 && options[0].MaxUploadSize > 0 {
		maxUploadSize = options[0].MaxUploadSize
	}
	server := &Server{
		appName:       optionsAppName(options),
		repositories:  repositories,
		canonicalURL:  strings.TrimRight(canonicalURL, "/"),
		maxUploadSize: maxUploadSize,
		resourceAuth:  optionsResourceAuth(options),
	}
	mux := http.NewServeMux()
	assets, err := fs.Sub(webFiles, "web/assets")
	if err != nil {
		panic(err)
	}
	mux.HandleFunc("GET /{$}", server.index)
	mux.HandleFunc("GET /repositories/{repository}", server.packages)
	mux.HandleFunc("GET /repositories/{repository}/{architecture}/packages/{package}", server.packageDetails)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
	mux.HandleFunc("GET /healthz", server.health)
	mux.Handle("GET "+cimd.PublisherMetadataPath, cimd.ProfileHandler(server.canonicalURL, cimd.PublisherMetadataPath, server.appName+" CLI - Publisher"))
	mux.Handle("GET "+cimd.AdminMetadataPath, cimd.ProfileHandler(server.canonicalURL, cimd.AdminMetadataPath, server.appName+" CLI - Admin"))
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", server.resourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/{resource...}", server.resourceMetadata)
	mux.HandleFunc("GET /repos/{repository}/{architecture}/{filename}", server.download)
	mux.HandleFunc("GET /api/v1/repositories", server.listRepositories)
	mux.Handle("POST /api/v1/repositories/{repository}", server.authenticate(auth.ScopeRepositoryCreate, http.HandlerFunc(server.createRepository)))
	mux.Handle("DELETE /api/v1/repositories/{repository}", server.authenticate(auth.ScopeRepositoryRemove, http.HandlerFunc(server.removeRepository)))
	mux.Handle("PATCH /api/v1/repositories/{repository}", server.authenticate(auth.ScopeRepositoryRename, http.HandlerFunc(server.renameRepository)))
	mux.HandleFunc("GET /api/v1/repositories/{repository}/{architecture}/packages", server.list)
	mux.Handle("POST /api/v1/repositories/{repository}/{architecture}/packages", server.authenticate(auth.ScopePublish, http.HandlerFunc(server.publish)))
	mux.Handle("DELETE /api/v1/repositories/{repository}/{architecture}/packages/{package}", server.authenticate(auth.ScopeRemove, http.HandlerFunc(server.remove)))
	return mux
}

func optionsResourceAuth(options []Options) *auth.ResourceServer {
	if len(options) > 0 {
		return options[0].ResourceAuth
	}
	return nil
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	repositories, err := s.repositories.Repositories()
	if err != nil {
		http.Error(w, "discover repositories", http.StatusInternalServerError)
		return
	}

	page := indexPage{AppName: s.appName, Repositories: repositories}
	if err := renderHTML(w, "index.html", page); err != nil {
		http.Error(w, "render repository index", http.StatusInternalServerError)
	}
}

func (s *Server) packages(w http.ResponseWriter, r *http.Request) {
	repositoryName := r.PathValue("repository")
	packages, err := s.repositories.ListRepository(repositoryName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	normalizedQuery := strings.ToLower(query)
	views, err := s.packageViews(repositoryName, packages)
	if err != nil {
		http.Error(w, "inspect package files", http.StatusInternalServerError)
		return
	}
	if normalizedQuery != "" {
		views = slices.DeleteFunc(views, func(view repositoryPackageView) bool {
			if strings.Contains(strings.ToLower(view.Name), normalizedQuery) {
				return false
			}
			for _, variant := range view.Variants {
				if strings.Contains(strings.ToLower(variant.Description), normalizedQuery) {
					return false
				}
			}
			return true
		})
	}

	page := packagesPage{
		AppName:       s.appName,
		Repository:    repositoryName,
		RepositoryURL: pathURL(s.canonicalURL, "repos", repositoryName, "$arch"),
		Query:         query,
		Packages:      views,
	}
	if err := renderHTML(w, "packages.html", page); err != nil {
		http.Error(w, "render package index", http.StatusInternalServerError)
	}
}

func (s *Server) packageDetails(w http.ResponseWriter, r *http.Request) {
	repositoryName := r.PathValue("repository")
	architecture := r.PathValue("architecture")
	packages, err := s.repositories.List(repositoryName, architecture)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	packageName := r.PathValue("package")
	index := slices.IndexFunc(packages, func(pkg alpm.Package) bool { return pkg.Name == packageName })
	if index < 0 {
		http.NotFound(w, r)
		return
	}
	views, err := s.packageViews(repositoryName, []repository.LocatedPackage{{
		TargetArchitecture: architecture,
		Package:            packages[index],
	}})
	if err != nil {
		http.Error(w, "inspect package files", http.StatusInternalServerError)
		return
	}
	page := packagePage{AppName: s.appName, Repository: repositoryName, Package: views[0]}
	if err := renderHTML(w, "package.html", page); err != nil {
		http.Error(w, "render package details", http.StatusInternalServerError)
	}
}

func optionsAppName(options []Options) string {
	if len(options) > 0 && options[0].AppName != "" {
		return options[0].AppName
	}
	return "PKGdepot"
}

func (s *Server) packageViews(repositoryName string, packages []repository.LocatedPackage) ([]repositoryPackageView, error) {
	views := make([]repositoryPackageView, 0)
	for _, pkg := range packages {
		if len(views) == 0 || views[len(views)-1].Name != pkg.Name {
			views = append(views, repositoryPackageView{
				Name:        pkg.Name,
				Description: pkg.Description,
				DetailsURL:  packageDetailsURL(repositoryName, pkg.TargetArchitecture, pkg.Name),
			})
		}

		downloadURL := repositoryFileURL(repositoryName, pkg.TargetArchitecture, pkg.Filename)
		hasSignature, err := s.repositories.HasSignature(repositoryName, pkg.TargetArchitecture, pkg.Filename)
		if err != nil {
			return nil, err
		}
		variant := packageVariantView{
			Package:            pkg.Package,
			TargetArchitecture: pkg.TargetArchitecture,
			FormattedSize:      formatBytes(pkg.Size),
			DetailsURL:         packageDetailsURL(repositoryName, pkg.TargetArchitecture, pkg.Name),
			DownloadURL:        downloadURL,
		}
		if hasSignature {
			variant.SignatureURL = downloadURL + ".sig"
		}
		view := &views[len(views)-1]
		if view.Description == "" {
			view.Description = pkg.Description
		}
		view.Variants = append(view.Variants, variant)
	}
	return views, nil
}

func repositoryFileURL(repositoryName, architecture, filename string) string {
	return pathURL("", "repos", repositoryName, architecture, filename)
}

func packageDetailsURL(repositoryName, architecture, packageName string) string {
	return pathURL("", "repositories", repositoryName, architecture, "packages", packageName)
}

func pathURL(base string, components ...string) string {
	endpoint, err := url.Parse(base)
	if err != nil {
		return ""
	}
	escapedComponents := make([]string, len(components))
	for index, component := range components {
		escapedComponents[index] = url.PathEscape(component)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/" + strings.Join(components, "/")
	endpoint.RawPath = strings.TrimRight(endpoint.EscapedPath(), "/") + "/" + strings.Join(escapedComponents, "/")
	return endpoint.String()
}

func formatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d bytes", size)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	value := float64(size)
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == units[len(units)-1] {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%d bytes", size)
}

func renderHTML(w http.ResponseWriter, name string, data any) error {
	var page bytes.Buffer
	if err := webTemplates.ExecuteTemplate(&page, name, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = page.WriteTo(w)
	return nil
}

func (s *Server) authenticate(permission string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.resourceAuth == nil || s.resourceAuth.Validator == nil {
			writeError(w, http.StatusUnauthorized, "authentication not configured")
			return
		}
		s.authenticateResource(w, r, permission, next)
	})
}

func (s *Server) authenticateResource(w http.ResponseWriter, r *http.Request, permission string, next http.Handler) {
	provided, err := auth.BearerToken(r.Header.Get("Authorization"))
	if errors.Is(err, auth.ErrMissingCredentials) {
		writeBearerChallenge(w, http.StatusUnauthorized, s.resourceRealm(), s.metadataURL())
		return
	}
	if errors.Is(err, auth.ErrInvalidRequest) {
		writeBearerError(w, http.StatusUnauthorized, "invalid_request", "the authorization header is malformed", "", s.resourceRealm(), s.metadataURL())
		return
	}
	claims, err := s.resourceAuth.Validator.Validate(r.Context(), provided)
	if err != nil {
		writeBearerError(w, http.StatusUnauthorized, "invalid_token", "the access token is invalid", "", s.resourceRealm(), s.metadataURL())
		return
	}
	authorized := s.resourceAuth.Authorize != nil && s.resourceAuth.Authorize(claims, permission, r.PathValue("repository"), r.PathValue("architecture"))
	if !authorized {
		writeBearerError(w, http.StatusForbidden, "insufficient_scope", "the token does not grant this operation", permission, s.resourceRealm(), s.metadataURL())
		return
	}
	next.ServeHTTP(w, r)
}

func (s *Server) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	if s.resourceAuth == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, s.resourceAuth.Metadata)
}

func (s *Server) metadataURL() string {
	if s.canonicalURL == "" {
		return ""
	}
	parsed, err := url.Parse(s.canonicalURL)
	if err != nil {
		return ""
	}
	resourcePath := strings.TrimRight(parsed.Path, "/")
	parsed.Path = "/.well-known/oauth-protected-resource" + resourcePath
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func writeBearerError(w http.ResponseWriter, status int, code, description, scope, realm, metadataURL string) {
	challenge := `Bearer error="` + quoteChallenge(code) + `", error_description="` + quoteChallenge(description) + `", realm="` + quoteChallenge(realm) + `"`
	if scope != "" {
		challenge += `, scope="` + quoteChallenge(scope) + `"`
	}
	if metadataURL != "" {
		challenge += `, resource_metadata="` + quoteChallenge(metadataURL) + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	writeJSON(w, status, api.ErrorResponse{Error: description, Code: code})
}

func writeBearerChallenge(w http.ResponseWriter, status int, realm, metadataURL string) {
	challenge := `Bearer realm="` + quoteChallenge(realm) + `"`
	if metadataURL != "" {
		challenge += `, resource_metadata="` + quoteChallenge(metadataURL) + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	writeError(w, status, "a bearer token is required")
}

func (s *Server) resourceRealm() string {
	if s.resourceAuth != nil && s.resourceAuth.Metadata.Resource != "" {
		return s.resourceAuth.Metadata.Resource
	}
	return s.canonicalURL
}

func quoteChallenge(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", "", "\n", "").Replace(value)
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
	response := make([]api.Package, 0, len(packages))
	for _, pkg := range packages {
		response = append(response, apiPackage(pkg))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) listRepositories(w http.ResponseWriter, _ *http.Request) {
	repositories, err := s.repositories.Repositories()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "discover repositories")
		return
	}
	response := make([]api.Repository, 0, len(repositories))
	for _, repository := range repositories {
		response = append(response, api.Repository{
			Name:          repository.Name,
			Architectures: repository.Architectures,
		})
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) createRepository(w http.ResponseWriter, r *http.Request) {
	repositoryName := r.PathValue("repository")
	if err := s.repositories.Create(repositoryName); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, api.Repository{Name: repositoryName, Architectures: []string{}})
}

func (s *Server) removeRepository(w http.ResponseWriter, r *http.Request) {
	if err := s.repositories.RemoveRepository(r.PathValue("repository")); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, repository.ErrRepositoryNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) renameRepository(w http.ResponseWriter, r *http.Request) {
	var request api.RenameRepositoryRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := decoder.Decode(&request); err != nil || request.Name == "" {
		writeError(w, http.StatusBadRequest, "request body must contain a non-empty name")
		return
	}
	if err := s.repositories.Rename(r.PathValue("repository"), request.Name); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, repository.ErrRepositoryNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadSize)
	upload, err := s.repositories.BeginUpload(r.PathValue("repository"), r.PathValue("architecture"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer upload.Cleanup()

	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload")
		return
	}
	packageFound := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart upload")
			return
		}
		switch part.FormName() {
		case "package":
			if packageFound || part.FileName() == "" {
				writeError(w, http.StatusBadRequest, "package form field is required exactly once")
				return
			}
			if err := upload.WritePackage(part.FileName(), part); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			packageFound = true
		case "signature":
			if part.FileName() == "" {
				writeError(w, http.StatusBadRequest, "invalid signature form field")
				return
			}
			if err := upload.WriteSignature(part); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		default:
			writeError(w, http.StatusBadRequest, "unexpected multipart form field")
			return
		}
	}
	if !packageFound {
		writeError(w, http.StatusBadRequest, "package form field is required")
		return
	}

	pkg, err := s.repositories.PublishUpload(r.Context(), r.PathValue("repository"), r.PathValue("architecture"), upload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, apiPackage(pkg))
}

func apiPackage(pkg alpm.Package) api.Package {
	return api.Package{
		Name:         pkg.Name,
		Version:      pkg.Version,
		Architecture: pkg.Architecture,
		Description:  pkg.Description,
		Filename:     pkg.Filename,
		Size:         pkg.Size,
		Depends:      pkg.Depends,
	}
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
	code := "internal_error"
	if status >= http.StatusBadRequest && status < http.StatusInternalServerError {
		code = "invalid_request"
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		code = "unauthorized"
	}
	if status == http.StatusNotFound {
		code = "not_found"
	}
	writeJSON(w, status, api.ErrorResponse{Error: fmt.Sprintf("%s", message), Code: code})
}
