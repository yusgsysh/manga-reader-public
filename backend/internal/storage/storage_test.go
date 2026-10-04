package storage

import (
	"fmt"
	"testing"
)

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"raw", ErrNotFound, true},
		{"wrapped", fmt.Errorf("storage: get \"images/x\": %w", ErrNotFound), true},
		{"double wrapped", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", ErrNotFound)), true},
		{"plain", fmt.Errorf("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFound(tt.err); got != tt.want {
				t.Errorf("IsNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestStorageInterfaceIsSatisfiedByBothDrivers(t *testing.T) {
	var _ Storage = (*LocalStorage)(nil)
	var _ Storage = (*S3Storage)(nil)
}
