package geodata

import (
	"encoding/binary"
	"fmt"
)

// Protobuf wire types. geodata messages use only these two; fixed-width
// fields are skipped if a newer file ever has them.
const (
	wireTypeVarint  = 0
	wireTypeFixed64 = 1
	wireTypeBytes   = 2
	wireTypeFixed32 = 5
)

// forEachField walks one message's fields in order. value is set for bytes
// fields and varint for varint fields; it slices the message, never copying.
func forEachField(message []byte, visit func(number int, wireType int, value []byte, varint uint64) error) error {
	for len(message) > 0 {
		key, keyLength := binary.Uvarint(message)
		if keyLength <= 0 {
			return fmt.Errorf("%w: bad field key", errMalformed)
		}
		message = message[keyLength:]
		number, wireType := int(key>>3), int(key&7)
		if number == 0 {
			return fmt.Errorf("%w: field number 0", errMalformed)
		}

		var (
			value  []byte
			varint uint64
		)
		switch wireType {
		case wireTypeVarint:
			decoded, length := binary.Uvarint(message)
			if length <= 0 {
				return fmt.Errorf("%w: bad varint", errMalformed)
			}
			varint = decoded
			message = message[length:]
		case wireTypeBytes:
			size, length := binary.Uvarint(message)
			if length <= 0 || size > uint64(len(message)-length) {
				return fmt.Errorf("%w: a field runs past the end", errMalformed)
			}
			value = message[length : length+int(size)]
			message = message[length+int(size):]
		case wireTypeFixed64:
			if len(message) < 8 {
				return fmt.Errorf("%w: a field runs past the end", errMalformed)
			}
			message = message[8:]
		case wireTypeFixed32:
			if len(message) < 4 {
				return fmt.Errorf("%w: a field runs past the end", errMalformed)
			}
			message = message[4:]
		default:
			return fmt.Errorf("%w: wire type %d", errMalformed, wireType)
		}

		if err := visit(number, wireType, value, varint); err != nil {
			return err
		}
	}
	return nil
}
