package security

import (
	"fmt"
	"os"
)

const (
	// RestrictedFileMode define permissão restrita de leitura/escrita apenas para o proprietário (0600).
	RestrictedFileMode os.FileMode = 0600

	// RestrictedDirMode define permissão restrita de diretório acessível apenas pelo proprietário (0700).
	RestrictedDirMode os.FileMode = 0700
)

// WriteRestrictedFile grava dados em um arquivo garantindo permissões 0600 e proteção contra TOCTOU via O_EXCL.
func WriteRestrictedFile(filePath string, data []byte) error {
	_ = os.Remove(filePath) // Remove link ou arquivo pré-existente antes da criação atômica contra TOCTOU
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, RestrictedFileMode)
	if err != nil {
		return fmt.Errorf("falha ao criar arquivo restrito %s: %w", filePath, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("falha ao escrever no arquivo restrito %s: %w", filePath, err)
	}

	// Força chmod caso umask tenha relaxado os bits de permissão
	if err := os.Chmod(filePath, RestrictedFileMode); err != nil {
		return fmt.Errorf("falha ao aplicar chmod restrito em %s: %w", filePath, err)
	}

	return nil
}

// CreateRestrictedTempDir cria um diretório temporário isolado com permissão restrita 0700.
func CreateRestrictedTempDir(prefix string) (string, error) {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", fmt.Errorf("falha ao criar diretório temporário restrito: %w", err)
	}

	if err := os.Chmod(dir, RestrictedDirMode); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("falha ao aplicar permissão 0700 no diretório temporário %s: %w", dir, err)
	}

	return dir, nil
}

// VerifyFilePermissions valida se o arquivo possui as permissões POSIX esperadas.
func VerifyFilePermissions(filePath string, expected os.FileMode) (bool, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return false, fmt.Errorf("falha ao verificar permissões de %s: %w", filePath, err)
	}

	perm := info.Mode().Perm()
	return perm == expected, nil
}
