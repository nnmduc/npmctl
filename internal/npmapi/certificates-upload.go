// Multipart certificate upload and validation.
//
// These two endpoints carry PEM material as form files rather than JSON. The
// output scrubber works on decoded objects keyed by field name, so a raw
// multipart body would walk straight past it — hence the deliberate rule here
// that these bodies are NEVER rendered. Only metadata is ever described.
package npmapi

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/nnmduc/npmctl/internal/exitcode"
)

// PEMFile is one uploaded file.
type PEMFile struct {
	Field    string
	Filename string
	Data     []byte
}

// Kind classifies the material WITHOUT revealing it, for previews.
func (f *PEMFile) Kind() string {
	s := string(f.Data)
	switch {
	case strings.Contains(s, "PRIVATE KEY"):
		return "private key"
	case strings.Contains(s, "BEGIN CERTIFICATE"):
		return "certificate"
	case strings.Contains(s, "BEGIN CERTIFICATE REQUEST"):
		return "certificate request"
	default:
		return "unrecognised"
	}
}

// CertificateFiles is the upload payload. certificate and certificate_key are
// required by the schema; intermediate_certificate is optional.
type CertificateFiles struct {
	Certificate    *PEMFile
	CertificateKey *PEMFile
	Intermediate   *PEMFile
}

// LoadCertificateFiles reads PEM files from disk.
func LoadCertificateFiles(certPath, keyPath, intermediatePath string) (*CertificateFiles, error) {
	out := &CertificateFiles{}
	var err error
	if out.Certificate, err = readPEM("certificate", certPath); err != nil {
		return nil, err
	}
	if out.CertificateKey, err = readPEM("certificate_key", keyPath); err != nil {
		return nil, err
	}
	if intermediatePath != "" {
		if out.Intermediate, err = readPEM("intermediate_certificate", intermediatePath); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func readPEM(field, path string) (*PEMFile, error) {
	if strings.TrimSpace(path) == "" {
		return nil, exitcode.New(exitcode.Usage, "%s file is required", field)
	}
	data, err := os.ReadFile(expandHome(path))
	if err != nil {
		return nil, exitcode.Wrap(exitcode.Usage, err, "read %s", field)
	}
	return &PEMFile{Field: field, Filename: filepath.Base(path), Data: data}, nil
}

// files returns the present files in schema order.
func (f *CertificateFiles) files() []*PEMFile {
	out := []*PEMFile{}
	for _, p := range []*PEMFile{f.Certificate, f.CertificateKey, f.Intermediate} {
		if p != nil {
			out = append(out, p)
		}
	}
	return out
}

// Describe renders upload metadata for a preview: field, filename, size and
// detected kind. It NEVER includes file contents — that is the whole point of
// having this method rather than rendering the body.
func (f *CertificateFiles) Describe() []map[string]any {
	out := make([]map[string]any, 0, 3)
	for _, p := range f.files() {
		out = append(out, map[string]any{
			"field":      p.Field,
			"filename":   p.Filename,
			"size_bytes": len(p.Data),
			"detected":   p.Kind(),
		})
	}
	return out
}

// Validate checks the payload locally before sending, so an obvious mix-up (a
// certificate passed as the key) is caught without a round trip.
func (f *CertificateFiles) Validate() error {
	if f.Certificate == nil || f.CertificateKey == nil {
		return exitcode.New(exitcode.Usage, "both --certificate and --certificate-key are required")
	}
	if f.CertificateKey.Kind() != "private key" {
		return exitcode.New(exitcode.Usage,
			"--certificate-key does not look like a private key (detected: %s)", f.CertificateKey.Kind())
	}
	if f.Certificate.Kind() != "certificate" {
		return exitcode.New(exitcode.Usage,
			"--certificate does not look like a certificate (detected: %s)", f.Certificate.Kind())
	}
	return nil
}

// multipartBody encodes the files as multipart/form-data.
func (f *CertificateFiles) multipartBody() (*rawBody, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range f.files() {
		part, err := w.CreateFormFile(p.Field, p.Filename)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(p.Data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return &rawBody{contentType: w.FormDataContentType(), data: buf.Bytes()}, nil
}

// UploadCertificate attaches custom PEM material to an existing certificate entry.
func (c *Client) UploadCertificate(ctx context.Context, id int, files *CertificateFiles) (any, error) {
	body, err := files.multipartBody()
	if err != nil {
		return nil, err
	}
	var out any
	req := request{
		method: "POST",
		path:   fmt.Sprintf("%s/%d/upload", certificatesPath, id),
		raw:    body,
	}
	if err := c.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ValidateCertificates asks NPM to parse PEM material without storing it.
func (c *Client) ValidateCertificates(ctx context.Context, files *CertificateFiles) (any, error) {
	body, err := files.multipartBody()
	if err != nil {
		return nil, err
	}
	var out any
	req := request{method: "POST", path: certificatesPath + "/validate", raw: body}
	if err := c.do(ctx, req, &out); err != nil {
		return nil, err
	}
	return out, nil
}
