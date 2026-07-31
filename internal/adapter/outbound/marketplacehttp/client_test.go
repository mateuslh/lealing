package marketplacehttp

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/marketplace"
)

func TestFetchAceitaJSONEntregueEmPartes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		flusher := writer.(http.Flusher)
		for _, part := range []string{`{"apiVersion":"`, marketplace.APIVersion, `","tools":[]}`} {
			_, _ = writer.Write([]byte(part))
			flusher.Flush()
		}
	}))
	defer server.Close()

	source := New(Config{Client: server.Client(), IndexURL: server.URL, AllowHTTP: true, TemporaryRoot: t.TempDir()})
	index, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if index.APIVersion != marketplace.APIVersion {
		t.Fatalf("apiVersion = %q", index.APIVersion)
	}
}

func TestFetchRejeitaPayloadGrandeEJSONInvalido(t *testing.T) {
	for name, body := range map[string]string{
		"grande": strings.Repeat("x", 65),
		"json":   `{"apiVersion":`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(body))
			}))
			defer server.Close()
			source := New(Config{Client: server.Client(), IndexURL: server.URL, AllowHTTP: true, IndexLimit: 64, TemporaryRoot: t.TempDir()})
			if _, err := source.Fetch(context.Background()); err == nil {
				t.Fatal("Fetch aceitou resposta inválida")
			}
		})
	}
}

func TestPrepareExtraiZIPDepoisDeValidarChecksum(t *testing.T) {
	archive := zipArchive(t, map[string]string{"manifest.yaml": "manifest", "demo.exe": "binário"})
	source, artifact := packageServer(t, "/demo.zip", archive)
	prepared, err := source.Prepare(context.Background(), artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Cleanup()
	raw, err := os.ReadFile(filepath.Join(prepared.Directory, "demo.exe"))
	if err != nil || string(raw) != "binário" {
		t.Fatalf("artefato = %q, %v", raw, err)
	}
}

func TestPrepareExtraiTarGZ(t *testing.T) {
	archive := tarArchive(t, map[string]string{"./manifest.yaml": "manifest", "./demo": "binário"})
	source, artifact := packageServer(t, "/demo.tar.gz", archive)
	prepared, err := source.Prepare(context.Background(), artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Cleanup()
	if _, err := os.Stat(filepath.Join(prepared.Directory, "manifest.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareRecusaChecksumDiferenteESemDeixarTemporario(t *testing.T) {
	archive := zipArchive(t, map[string]string{"manifest.yaml": "manifest"})
	source, artifact := packageServer(t, "/demo.zip", archive)
	artifact.SHA256 = strings.Repeat("0", 64)
	if _, err := source.Prepare(context.Background(), artifact); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Prepare = %v", err)
	}
	entries, err := os.ReadDir(source.config.TemporaryRoot)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporários após falha = %v, %v", entries, err)
	}
}

func TestPrepareRecusaTraversalEEntradaSimbolica(t *testing.T) {
	for name, archive := range map[string][]byte{
		"traversal": zipArchive(t, map[string]string{"../escape": "ataque"}),
		"symlink":   zipSymlink(t),
	} {
		t.Run(name, func(t *testing.T) {
			source, artifact := packageServer(t, "/demo.zip", archive)
			if _, err := source.Prepare(context.Background(), artifact); err == nil {
				t.Fatal("Prepare aceitou entrada insegura")
			}
		})
	}
}

func packageServer(t *testing.T, name string, body []byte) (*Source, marketplace.Artifact) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != name {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write(body)
	}))
	t.Cleanup(server.Close)
	sum := sha256.Sum256(body)
	source := New(Config{Client: server.Client(), AllowHTTP: true, TemporaryRoot: t.TempDir()})
	return source, marketplace.Artifact{
		Platform: "windows-amd64", URL: server.URL + name, SHA256: hex.EncodeToString(sum[:]),
	}
}

func zipArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.Write([]byte(content))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func zipSymlink(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	header := &zip.FileHeader{Name: "link", Method: zip.Store}
	header.SetMode(os.ModeSymlink | 0o777)
	file, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprint(file, "manifest.yaml")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func tarArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	if err := writer.WriteHeader(&tar.Header{Name: "./", Mode: 0o700, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		raw := []byte(content)
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(raw); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
