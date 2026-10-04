package dns

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/The-Apranik/Apranik/spec"
)

const (
	// The EDNS(0) UDP payload size the 2020 DNS flag day settled on: large
	// enough for nearly every answer, small enough to avoid IP fragmentation.
	advertisedUDPPayloadSize = 1232

	// RFC 8467's recommended block for query padding (RFC 7830), so the size
	// of an encrypted query does not give away the name's length.
	queryPaddingBlockSize = 128
	ednsPaddingOptionCode = 12

	// A longer CNAME chain is a misconfiguration or an attack.
	maxCNAMEChainLength = 16

	// RFC 2308 lets a negative answer be cached for its SOA minimum. Capped
	// so a forged SOA cannot black-hole a name for long. PROVISIONAL.
	maxNegativeAnswerTTL     = 5 * time.Minute
	defaultNegativeAnswerTTL = 30 * time.Second

	// RFC 2181 section 8: a TTL with the top bit set is treated as zero.
	maxValidTTLSeconds = 1<<31 - 1
)

var (
	errResponseIDMismatch       = errors.New("dns: response does not answer this query (ID differs)")
	errResponseQuestionMismatch = errors.New("dns: response does not answer this query (question differs)")
	errNotAResponse             = errors.New("dns: message is not a response")
	errCNAMEChainTooLong        = errors.New("dns: CNAME chain too long")
)

// ServerFailureError is an answer that says the server could not answer:
// SERVFAIL, REFUSED and the like. It is not cached.
type ServerFailureError struct {
	ResponseCode dnsmessage.RCode
}

func (e *ServerFailureError) Error() string {
	return fmt.Sprintf("dns: server answered %s", e.ResponseCode)
}

// answer is what one query learned about one name and one record type.
type answer struct {
	addresses []netip.Addr

	// The name exists without records of this type, or does not exist.
	negative bool

	// How long the answer may be reused; zero means not at all.
	ttl time.Duration
}

func newQueryID() uint16 {
	var random [2]byte
	rand.Read(random[:])
	return binary.BigEndian.Uint16(random[:])
}

// newQueryMessage builds a recursive query for one name and record type, with
// EDNS(0) and padding. name is fully qualified.
func newQueryMessage(id uint16, name dnsmessage.Name, recordType dnsmessage.Type) ([]byte, error) {
	unpadded, err := buildQueryMessage(id, name, recordType, 0)
	if err != nil {
		return nil, err
	}
	// The padding option adds a 4-byte header before its data.
	paddedLength := (len(unpadded) + 4 + queryPaddingBlockSize - 1) / queryPaddingBlockSize * queryPaddingBlockSize
	return buildQueryMessage(id, name, recordType, paddedLength-len(unpadded)-4)
}

func buildQueryMessage(id uint16, name dnsmessage.Name, recordType dnsmessage.Type, paddingLength int) ([]byte, error) {
	builder := dnsmessage.NewBuilder(make([]byte, 0, queryPaddingBlockSize), dnsmessage.Header{
		ID:               id,
		RecursionDesired: true,
	})
	builder.EnableCompression()

	if err := builder.StartQuestions(); err != nil {
		return nil, err
	}
	if err := builder.Question(dnsmessage.Question{Name: name, Type: recordType, Class: dnsmessage.ClassINET}); err != nil {
		return nil, err
	}
	if err := builder.StartAdditionals(); err != nil {
		return nil, err
	}

	var optHeader dnsmessage.ResourceHeader
	if err := optHeader.SetEDNS0(advertisedUDPPayloadSize, dnsmessage.RCodeSuccess, false); err != nil {
		return nil, err
	}
	var options []dnsmessage.Option
	if paddingLength > 0 {
		options = []dnsmessage.Option{{Code: ednsPaddingOptionCode, Data: make([]byte, paddingLength)}}
	}
	if err := builder.OPTResource(optHeader, dnsmessage.OPTResource{Options: options}); err != nil {
		return nil, err
	}
	return builder.Finish()
}

// readAnswer checks that response answers exactly the query sent and returns
// the addresses of the queried name, following its CNAME chain. Records for
// any other owner are ignored: a server, or someone injecting packets, cannot
// slip in addresses for a name nobody asked about.
func readAnswer(response []byte, queryID uint16, name dnsmessage.Name, recordType dnsmessage.Type) (answer, error) {
	var parser dnsmessage.Parser
	header, err := parser.Start(response)
	if err != nil {
		return answer{}, fmt.Errorf("dns: malformed response: %w", err)
	}
	if !header.Response {
		return answer{}, errNotAResponse
	}
	if header.ID != queryID {
		return answer{}, errResponseIDMismatch
	}
	if header.Truncated {
		return answer{}, spec.ErrDNSAnswerTruncated
	}

	questions, err := parser.AllQuestions()
	if err != nil {
		return answer{}, fmt.Errorf("dns: malformed response: %w", err)
	}
	if len(questions) != 1 || questions[0].Type != recordType || questions[0].Class != dnsmessage.ClassINET ||
		!sameName(questions[0].Name, name) {
		return answer{}, errResponseQuestionMismatch
	}

	switch header.RCode {
	case dnsmessage.RCodeSuccess:
	case dnsmessage.RCodeNameError:
		return negativeAnswer(&parser, false), nil
	default:
		return answer{}, &ServerFailureError{ResponseCode: header.RCode}
	}

	records, err := parser.AllAnswers()
	if err != nil {
		return answer{}, fmt.Errorf("dns: malformed response: %w", err)
	}

	owner := lowerName(name)
	ttlSeconds := uint32(maxValidTTLSeconds)
	for range maxCNAMEChainLength + 1 {
		target, cnameTTL, found := cnameOf(records, owner)
		if !found {
			break
		}
		ttlSeconds = min(ttlSeconds, cnameTTL)
		owner = target
	}
	if _, _, stillAlias := cnameOf(records, owner); stillAlias {
		return answer{}, errCNAMEChainTooLong
	}

	var addresses []netip.Addr
	for _, record := range records {
		if record.Header.Class != dnsmessage.ClassINET || record.Header.Type != recordType ||
			lowerName(record.Header.Name) != owner {
			continue
		}
		switch body := record.Body.(type) {
		case *dnsmessage.AResource:
			addresses = append(addresses, netip.AddrFrom4(body.A))
		case *dnsmessage.AAAAResource:
			addresses = append(addresses, netip.AddrFrom16(body.AAAA))
		default:
			continue
		}
		ttlSeconds = min(ttlSeconds, validTTL(record.Header.TTL))
	}
	if len(addresses) == 0 {
		return negativeAnswer(&parser, true), nil
	}
	return answer{addresses: addresses, ttl: time.Duration(ttlSeconds) * time.Second}, nil
}

// negativeAnswer reads the SOA a server puts in the authority section, whose
// TTL and minimum bound how long "no such record" may be cached (RFC 2308).
// A malformed authority section only costs the SOA's TTL, not the answer.
func negativeAnswer(parser *dnsmessage.Parser, answersRead bool) answer {
	ttl := defaultNegativeAnswerTTL
	if !answersRead {
		if err := parser.SkipAllAnswers(); err != nil {
			return answer{negative: true, ttl: ttl}
		}
	}
	if authorities, err := parser.AllAuthorities(); err == nil {
		for _, record := range authorities {
			if soa, isSOA := record.Body.(*dnsmessage.SOAResource); isSOA {
				seconds := min(validTTL(record.Header.TTL), validTTL(soa.MinTTL))
				ttl = time.Duration(seconds) * time.Second
				break
			}
		}
	}
	return answer{negative: true, ttl: min(ttl, maxNegativeAnswerTTL)}
}

func cnameOf(records []dnsmessage.Resource, owner string) (target string, ttlSeconds uint32, found bool) {
	for _, record := range records {
		if record.Header.Type != dnsmessage.TypeCNAME || record.Header.Class != dnsmessage.ClassINET {
			continue
		}
		cname, isCNAME := record.Body.(*dnsmessage.CNAMEResource)
		if !isCNAME || lowerName(record.Header.Name) != owner {
			continue
		}
		return lowerName(cname.CNAME), validTTL(record.Header.TTL), true
	}
	return "", 0, false
}

func validTTL(seconds uint32) uint32 {
	if seconds > maxValidTTLSeconds {
		return 0
	}
	return seconds
}

func lowerName(name dnsmessage.Name) string { return strings.ToLower(name.String()) }

func sameName(first, second dnsmessage.Name) bool {
	return strings.EqualFold(first.String(), second.String())
}
