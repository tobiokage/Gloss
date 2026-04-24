package hdfc

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
)

func EncryptPayload(plainJSON []byte, clientSecretKeyHex string, iv string) (string, error) {
	block, ivBytes, err := newCipherBlock(clientSecretKeyHex, iv)
	if err != nil {
		return "", err
	}

	padded := pkcs5Pad(plainJSON, block.BlockSize())
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, ivBytes).CryptBlocks(encrypted, padded)

	return hex.EncodeToString(encrypted), nil
}

func DecryptPayload(encryptedHex string, clientSecretKeyHex string, iv string) ([]byte, error) {
	block, ivBytes, err := newCipherBlock(clientSecretKeyHex, iv)
	if err != nil {
		return nil, err
	}

	encrypted, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return nil, fmt.Errorf("invalid encrypted payload hex: %w", err)
	}
	if len(encrypted) == 0 || len(encrypted)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("invalid encrypted payload length")
	}

	plainPadded := make([]byte, len(encrypted))
	cipher.NewCBCDecrypter(block, ivBytes).CryptBlocks(plainPadded, encrypted)

	return pkcs5Unpad(plainPadded, block.BlockSize())
}

func newCipherBlock(clientSecretKeyHex string, iv string) (cipher.Block, []byte, error) {
	key, err := hex.DecodeString(clientSecretKeyHex)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid HDFC client secret key: %w", err)
	}
	if len(key) != 32 {
		return nil, nil, fmt.Errorf("HDFC client secret key must decode to 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize HDFC cipher: %w", err)
	}

	ivBytes := []byte(iv)
	if len(ivBytes) != block.BlockSize() {
		return nil, nil, fmt.Errorf("HDFC IV must be %d bytes", block.BlockSize())
	}

	return block, ivBytes, nil
}

func pkcs5Pad(input []byte, blockSize int) []byte {
	paddingLength := blockSize - len(input)%blockSize
	padded := make([]byte, len(input)+paddingLength)
	copy(padded, input)
	for i := len(input); i < len(padded); i++ {
		padded[i] = byte(paddingLength)
	}
	return padded
}

func pkcs5Unpad(input []byte, blockSize int) ([]byte, error) {
	if len(input) == 0 || len(input)%blockSize != 0 {
		return nil, fmt.Errorf("invalid padded payload length")
	}

	paddingLength := int(input[len(input)-1])
	if paddingLength == 0 || paddingLength > blockSize || paddingLength > len(input) {
		return nil, fmt.Errorf("invalid padded payload")
	}

	for i := len(input) - paddingLength; i < len(input); i++ {
		if input[i] != byte(paddingLength) {
			return nil, fmt.Errorf("invalid padded payload")
		}
	}

	return input[:len(input)-paddingLength], nil
}
