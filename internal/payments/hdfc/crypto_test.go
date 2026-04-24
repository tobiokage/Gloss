package hdfc

import "testing"

func TestEncryptDecryptPayloadRoundTrip(t *testing.T) {
	secret := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	iv := "1234567890abcdef"
	plain := []byte(`{"saleTxnId":"SALE123","saleAmount":"10.50"}`)

	encrypted, err := EncryptPayload(plain, secret, iv)
	if err != nil {
		t.Fatalf("EncryptPayload returned error: %v", err)
	}
	if encrypted == "" {
		t.Fatal("expected encrypted payload")
	}

	decrypted, err := DecryptPayload(encrypted, secret, iv)
	if err != nil {
		t.Fatalf("DecryptPayload returned error: %v", err)
	}
	if string(decrypted) != string(plain) {
		t.Fatalf("decrypted payload = %s, want %s", string(decrypted), string(plain))
	}
}
