package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureRuntimeDir(t *testing.T) {
	dir, err := ensureRuntimeDir()
	if err != nil {
		t.Fatalf("ensureRuntimeDir falhou: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("falha ao obter info do diretório de runtime: %v", err)
	}

	if !info.IsDir() {
		t.Errorf("caminho de runtime não é diretório: %s", dir)
	}

	// Valida permissão estrita 0700
	perm := info.Mode().Perm()
	if perm != 0700 {
		t.Errorf("permissão do diretório de runtime incorreta: obtido %04o, esperado 0700", perm)
	}
}

func TestWriteSecureFile(t *testing.T) {
	dir, err := ensureRuntimeDir()
	if err != nil {
		t.Fatalf("falha ao obter runtime dir: %v", err)
	}

	testFile := filepath.Join(dir, "test-secure.txt")
	defer os.Remove(testFile)

	content := []byte("secret agent prompt payload")
	if err := writeSecureFile(testFile, content); err != nil {
		t.Fatalf("writeSecureFile falhou: %v", err)
	}

	info, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("falha ao obter info do arquivo: %v", err)
	}

	// Valida permissão estrita 0600
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("permissão do arquivo incorreta: obtido %04o, esperado 0600", perm)
	}

	// Valida reescrita atômica segura
	newContent := []byte("updated payload")
	if err := writeSecureFile(testFile, newContent); err != nil {
		t.Fatalf("falha ao regravar arquivo seguro: %v", err)
	}

	readData, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("falha ao ler arquivo: %v", err)
	}
	if string(readData) != string(newContent) {
		t.Errorf("conteúdo lido diverge do gravado: %q", string(readData))
	}
}

func TestPidFilePath(t *testing.T) {
	path, err := pidFilePath("test-session")
	if err != nil {
		t.Fatalf("pidFilePath falhou: %v", err)
	}

	if !strings.HasSuffix(path, "test-session.pid") {
		t.Errorf("caminho de pid não possui sufixo esperado: %s", path)
	}
}
