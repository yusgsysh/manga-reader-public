package main

import (
	"log/slog"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"", slog.LevelWarn},
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"invalid", slog.LevelWarn},
		{"  warn  ", slog.LevelWarn},
		{"  DEBUG  ", slog.LevelDebug},
		{"unknown", slog.LevelWarn},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLogLevel(tt.input)
			if got != tt.expected {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseLogLevel_DefaultIsWarn(t *testing.T) {
	got := parseLogLevel("")
	if got != slog.LevelWarn {
		t.Errorf("parseLogLevel(\"\") = %v, want LevelWarn", got)
	}
}

func TestParseLogLevel_CaseInsensitive(t *testing.T) {
	tests := []string{"Warn", "WARN", "wArN", "Warning", "WARNING", "warning"}
	for _, input := range tests {
		got := parseLogLevel(input)
		if got != slog.LevelWarn {
			t.Errorf("parseLogLevel(%q) = %v, want LevelWarn", input, got)
		}
	}
}

func TestParseLogLevel_TrimSpace(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"  debug  ", slog.LevelDebug},
		{"\tinfo\n", slog.LevelInfo},
		{"  warn  ", slog.LevelWarn},
		{" error ", slog.LevelError},
	}
	for _, tt := range tests {
		got := parseLogLevel(tt.input)
		if got != tt.expected {
			t.Errorf("parseLogLevel(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}
