package sandbox

import (
	"context"
	"fmt"
	"os"

	"github.com/veertuinc/crypt/internal/anka"
)

// Destroy stops and deletes a crypt run VM.
func Destroy(ctx context.Context, baseVM, name string) error {
	if name == "" || name == baseVM {
		return fmt.Errorf("refusing to destroy %q", name)
	}

	client := anka.New()
	if !client.Installed() {
		return fmt.Errorf("the `anka` CLI was not found on your PATH; install Anka from https://veertu.com/download-anka-build/")
	}

	exists, err := client.Exists(ctx, name)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("VM %q not found", name)
	}

	fmt.Fprintf(os.Stderr, "crypt: destroying %s\n", name)
	_ = client.Stop(ctx, name)
	if err := client.Delete(ctx, name); err != nil {
		return fmt.Errorf("deleting %s: %w", name, err)
	}
	return nil
}
