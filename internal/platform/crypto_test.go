package platform

import "testing"

func TestSealAndOpenSecret(t *testing.T) {
	master := []byte("test-master-key")
	payload := []byte(`{"hello":"world"}`)
	sealed, err := SealSecret(master, payload)
	if err != nil {
		t.Fatalf("SealSecret() error = %v", err)
	}
	opened, err := OpenSecret(master, sealed)
	if err != nil {
		t.Fatalf("OpenSecret() error = %v", err)
	}
	if string(opened) != string(payload) {
		t.Fatalf("opened payload = %s, want %s", string(opened), string(payload))
	}
}
