package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/trly/pkgdepot/internal/alpm"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: http.DefaultClient}
}

func (c *Client) Publish(ctx context.Context, repository, architecture, packagePath, signaturePath string) (alpm.Package, error) {
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	request, err := c.request(ctx, http.MethodPost, c.packagesURL(repository, architecture), reader)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return alpm.Package{}, err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	writeResult := make(chan error, 1)
	go func() {
		err := addFile(multipartWriter, "package", packagePath)
		if err == nil && signaturePath != "" {
			err = addFile(multipartWriter, "signature", signaturePath)
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		_ = writer.CloseWithError(err)
		writeResult <- err
	}()

	var pkg alpm.Package
	requestErr := c.do(request, &pkg)
	writeErr := <-writeResult
	if writeErr != nil && (requestErr == nil || !errors.Is(writeErr, io.ErrClosedPipe)) {
		return alpm.Package{}, writeErr
	}
	if requestErr != nil {
		return alpm.Package{}, requestErr
	}
	return pkg, nil
}

func (c *Client) List(ctx context.Context, repository, architecture string) ([]alpm.Package, error) {
	request, err := c.request(ctx, http.MethodGet, c.packagesURL(repository, architecture), nil)
	if err != nil {
		return nil, err
	}
	var packages []alpm.Package
	if err := c.do(request, &packages); err != nil {
		return nil, err
	}
	return packages, nil
}

func (c *Client) Remove(ctx context.Context, repository, architecture, packageName string) error {
	endpoint := c.packagesURL(repository, architecture) + "/" + url.PathEscape(packageName)
	request, err := c.request(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	return c.do(request, nil)
}

func (c *Client) packagesURL(repository, architecture string) string {
	return c.BaseURL + "/api/v1/repositories/" + url.PathEscape(repository) + "/" + url.PathEscape(architecture) + "/packages"
}

func (c *Client) request(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.Token)
	return request, nil
}

func (c *Client) do(request *http.Request, destination any) error {
	response, err := c.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(response.Body).Decode(&apiError); err != nil || apiError.Error == "" {
			return fmt.Errorf("server returned %s", response.Status)
		}
		return fmt.Errorf("server returned %s: %s", response.Status, apiError.Error)
	}
	if destination == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func addFile(writer *multipart.Writer, field, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open %s: %w", field, err)
	}
	defer file.Close()
	part, err := writer.CreateFormFile(field, path.Base(filename))
	if err != nil {
		return fmt.Errorf("create %s form field: %w", field, err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("write %s form field: %w", field, err)
	}
	return nil
}
