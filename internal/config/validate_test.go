package config

import "testing"

func TestValidateServerURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"empty", "", "", true},
		{"valid https", "https://host:8080", "https://host:8080", false},
		{"valid http", "http://localhost:8080", "http://localhost:8080", false},
		{"trailing slash trimmed", "https://host:8080/", "https://host:8080", false},
		{"missing scheme", "host:8080", "", true},
		{"bad scheme", "ftp://host:8080", "", true},
		{"missing host", "https://", "", true},
		{"malformed", "http://[::1", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateServerURL(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateServerURL(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ValidateServerURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestValidateToken(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"empty", "", true},
		{"too short", "short", true},
		{"long enough", "a-reasonably-long-random-token", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateToken(tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateToken(%q) error = %v, wantErr %v", tt.token, err, tt.wantErr)
			}
		})
	}
}

func TestValidateListen(t *testing.T) {
	tests := []struct {
		name    string
		listen  string
		wantErr bool
	}{
		{"empty", "", true},
		{"valid with host", "127.0.0.1:8080", false},
		{"valid wildcard", ":8080", false},
		{"missing port", "127.0.0.1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateListen(tt.listen)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateListen(%q) error = %v, wantErr %v", tt.listen, err, tt.wantErr)
			}
		})
	}
}

func TestValidateDataDir(t *testing.T) {
	if err := ValidateDataDir(""); err == nil {
		t.Error("ValidateDataDir(\"\"): want error")
	}
	if err := ValidateDataDir("/var/lib/freetube-sync"); err != nil {
		t.Errorf("ValidateDataDir: unexpected error: %v", err)
	}
}
