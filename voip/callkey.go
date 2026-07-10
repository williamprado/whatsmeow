// Copyright (c) 2026 William Prado
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package voip

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// CallKeyLen is the length of the symmetric call key.
const CallKeyLen = 32

// GenerateCallKey returns a fresh 32-byte symmetric call key.
func GenerateCallKey() []byte {
	b := make([]byte, CallKeyLen)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// GenerateCallID returns a random 16-byte uppercase-hex call id.
func GenerateCallID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b))
}

// encodeCallKeyMessage wraps the call key in waE2E.Message{Call:{CallKey}} and
// marshals it — the plaintext that is Signal-encrypted per device.
func encodeCallKeyMessage(callKey []byte) ([]byte, error) {
	return proto.Marshal(&waE2E.Message{Call: &waE2E.Call{CallKey: callKey}})
}

// decodeCallKeyPlaintext extracts the 32-byte call key from a decrypted
// waE2E.Message.
func decodeCallKeyPlaintext(plaintext []byte) ([]byte, error) {
	var msg waE2E.Message
	if err := proto.Unmarshal(plaintext, &msg); err != nil {
		return nil, err
	}
	key := msg.GetCall().GetCallKey()
	if len(key) != CallKeyLen {
		return nil, fmt.Errorf("voip: invalid callKey: expected %d bytes, got %d", CallKeyLen, len(key))
	}
	return key, nil
}
