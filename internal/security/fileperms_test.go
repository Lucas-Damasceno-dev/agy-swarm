package security_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Lucas-Damasceno-dev/agy-swarm/internal/security"
)

func TestWriteRestrictedFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sec-test-*")
	if err != nil {
		t.Fatalf("falha ao criar tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	filePath := filepath.Join(tempDir, "restricted.txt")
	content := []byte("conteúdo confidencial")

	if err := security.WriteRestrictedFile(filePath, content); err != nil {
		t.Fatalf("WriteRestrictedFile falhou: %v", err)
	}

	ok, err := security.VerifyFilePermissions(filePath, security.RestrictedFileMode)
	if err != nil {
		t.Fatalf("VerifyFilePermissions falhou: %v", err)
	}
	if !ok {
		info, _ := os.Stat(filePath)
		t.Errorf("esperava permissões 0600, obteve %#o", info.Mode().Perm())
	}

	read, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("erro ao ler arquivo gravado: %v", err)
	}
	if string(read) != string(content) {
		t.Errorf("conteúdo lido diverge do gravado: %s", string(read))
	}
}

func TestCreateRestrictedTempDir(t *testing.T) {
	dir, err := security.CreateRestrictedTempDir("sec-restricted-dir-*")
	if err != nil {
		t.Fatalf("CreateRestrictedTempDir falhou: %v", err)
	}
	defer os.RemoveAll(dir)

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("os.Stat falhou no diretório: %v", err)
	}

	if perm := info.Mode().Perm(); perm != security.RestrictedDirMode {
		t.Errorf("esperava permissão 0700 para diretório, obteve %#o", perm)
	}
}
