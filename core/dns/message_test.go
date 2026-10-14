package dns

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/we-apranik/apranik/spec"
)

func TestQueryMessageIsPaddedRecursiveAndAdvertisesEDNS(t *testing.T) {
	for _, name := range []string{"a.io.", "example.com.", "a-very-long-label-for-padding.subdomain.example.org."} {
		message, err := newQueryMessage(0x1234, mustName(name), dnsmessage.TypeAAAA)
		if err != nil {
			t.Fatal(err)
		}
		if len(message)%queryPaddingBlockSize != 0 {
			t.Errorf("%s: query is %d bytes, want a multiple of %d", name, len(message), queryPaddingBlockSize)
		}

		var parsed dnsmessage.Message
		if err := parsed.Unpack(message); err != nil {
			t.Fatal(err)
		}
		if parsed.ID != 0x1234 || !parsed.RecursionDesired || parsed.Response {
			t.Errorf("%s: header %+v", name, parsed.Header)
		}
		if len(parsed.Questions) != 1 || parsed.Questions[0].Name.String() != name || parsed.Questions[0].Type != dnsmessage.TypeAAAA {
			t.Errorf("%s: questions %+v", name, parsed.Questions)
		}
		if len(parsed.Additionals) != 1 || parsed.Additionals[0].Header.Type != dnsmessage.TypeOPT ||
			parsed.Additionals[0].Header.Class != advertisedUDPPayloadSize {
			t.Errorf("%s: want one OPT record advertising %d bytes, got %+v", name, advertisedUDPPayloadSize, parsed.Additionals)
		}
	}
}

func TestQueryIDsDiffer(t *testing.T) {
	seen := map[uint16]bool{}
	for range 64 {
		seen[newQueryID()] = true
	}
	if len(seen) < 60 {
		t.Fatalf("64 query IDs had only %d distinct values", len(seen))
	}
}

type responseOptions struct {
	id            uint16
	notResponse   bool
	truncated     bool
	rcode         dnsmessage.RCode
	question      dnsmessage.Question
	answers       []dnsmessage.Resource
	authorities   []dnsmessage.Resource
	skipQuestions bool
}

func buildResponse(t *testing.T, options responseOptions) []byte {
	t.Helper()
	builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID: options.id, Response: !options.notResponse, Truncated: options.truncated, RCode: options.rcode,
	})
	builder.StartQuestions()
	if !options.skipQuestions {
		builder.Question(options.question)
	}
	builder.StartAnswers()
	for _, record := range options.answers {
		addRecord(t, &builder, record)
	}
	builder.StartAuthorities()
	for _, record := range options.authorities {
		addRecord(t, &builder, record)
	}
	message, err := builder.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func addRecord(t *testing.T, builder *dnsmessage.Builder, record dnsmessage.Resource) {
	t.Helper()
	var err error
	switch body := record.Body.(type) {
	case *dnsmessage.AResource:
		err = builder.AResource(record.Header, *body)
	case *dnsmessage.AAAAResource:
		err = builder.AAAAResource(record.Header, *body)
	case *dnsmessage.CNAMEResource:
		err = builder.CNAMEResource(record.Header, *body)
	case *dnsmessage.SOAResource:
		err = builder.SOAResource(record.Header, *body)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func aRecord(owner string, ttl uint32, address string) dnsmessage.Resource {
	return dnsmessage.Resource{
		Header: dnsmessage.ResourceHeader{Name: mustName(owner), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: ttl},
		Body:   &dnsmessage.AResource{A: netip.MustParseAddr(address).As4()},
	}
}

func cnameRecord(owner string, ttl uint32, target string) dnsmessage.Resource {
	return dnsmessage.Resource{
		Header: dnsmessage.ResourceHeader{Name: mustName(owner), Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: ttl},
		Body:   &dnsmessage.CNAMEResource{CNAME: mustName(target)},
	}
}

func soaRecord(ttl, minimum uint32) dnsmessage.Resource {
	return dnsmessage.Resource{
		Header: dnsmessage.ResourceHeader{Name: mustName("example.com"), Type: dnsmessage.TypeSOA, Class: dnsmessage.ClassINET, TTL: ttl},
		Body: &dnsmessage.SOAResource{NS: mustName("ns.example.com"), MBox: mustName("admin.example.com"),
			Serial: 1, Refresh: 1, Retry: 1, Expire: 1, MinTTL: minimum},
	}
}

func TestReadAnswer(t *testing.T) {
	queried := mustName("www.example.com")
	question := dnsmessage.Question{Name: queried, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}

	for _, tc := range []struct {
		name          string
		options       responseOptions
		wantAddresses []string
		wantNegative  bool
		wantTTL       time.Duration
		wantErr       error
		wantAnyErr    bool
	}{
		{
			name:          "addresses",
			options:       responseOptions{id: 7, question: question, answers: []dnsmessage.Resource{aRecord("www.example.com", 300, "192.0.2.1"), aRecord("www.example.com", 60, "192.0.2.2")}},
			wantAddresses: []string{"192.0.2.1", "192.0.2.2"},
			wantTTL:       60 * time.Second,
		},
		{
			name: "case of the owner name does not matter",
			options: responseOptions{id: 7, question: dnsmessage.Question{Name: mustName("WWW.Example.COM"), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
				answers: []dnsmessage.Resource{aRecord("Www.EXAMPLE.com", 300, "192.0.2.1")}},
			wantAddresses: []string{"192.0.2.1"},
			wantTTL:       300 * time.Second,
		},
		{
			name: "a CNAME chain is followed and its shortest TTL kept",
			options: responseOptions{id: 7, question: question, answers: []dnsmessage.Resource{
				cnameRecord("www.example.com", 30, "edge.cdn.net"),
				cnameRecord("edge.cdn.net", 600, "node.cdn.net"),
				aRecord("node.cdn.net", 120, "198.51.100.9"),
			}},
			wantAddresses: []string{"198.51.100.9"},
			wantTTL:       30 * time.Second,
		},
		{
			name: "records for a name nobody asked about are ignored",
			options: responseOptions{id: 7, question: question, answers: []dnsmessage.Resource{
				aRecord("bank.example", 300, "203.0.113.66"),
				aRecord("www.example.com", 300, "192.0.2.1"),
			}},
			wantAddresses: []string{"192.0.2.1"},
			wantTTL:       300 * time.Second,
		},
		{
			name:         "only foreign records is no data",
			options:      responseOptions{id: 7, question: question, answers: []dnsmessage.Resource{aRecord("bank.example", 300, "203.0.113.66")}},
			wantNegative: true,
			wantTTL:      defaultNegativeAnswerTTL,
		},
		{
			name:         "name error takes the SOA's smaller TTL",
			options:      responseOptions{id: 7, rcode: dnsmessage.RCodeNameError, question: question, authorities: []dnsmessage.Resource{soaRecord(90, 45)}},
			wantNegative: true,
			wantTTL:      45 * time.Second,
		},
		{
			name:         "a negative TTL is capped",
			options:      responseOptions{id: 7, rcode: dnsmessage.RCodeNameError, question: question, authorities: []dnsmessage.Resource{soaRecord(86400, 86400)}},
			wantNegative: true,
			wantTTL:      maxNegativeAnswerTTL,
		},
		{
			name:         "no data after a CNAME reads the SOA",
			options:      responseOptions{id: 7, question: question, answers: []dnsmessage.Resource{cnameRecord("www.example.com", 300, "v6only.example.com")}, authorities: []dnsmessage.Resource{soaRecord(20, 600)}},
			wantNegative: true,
			wantTTL:      20 * time.Second,
		},
		{
			name:          "a TTL with the top bit set is zero",
			options:       responseOptions{id: 7, question: question, answers: []dnsmessage.Resource{aRecord("www.example.com", 1<<31, "192.0.2.1")}},
			wantAddresses: []string{"192.0.2.1"},
			wantTTL:       0,
		},
		{name: "another ID", options: responseOptions{id: 8, question: question}, wantErr: errResponseIDMismatch},
		{name: "a query, not a response", options: responseOptions{id: 7, notResponse: true, question: question}, wantErr: errNotAResponse},
		{name: "truncated", options: responseOptions{id: 7, truncated: true, question: question}, wantErr: spec.ErrDNSAnswerTruncated},
		{
			name:    "another name",
			options: responseOptions{id: 7, question: dnsmessage.Question{Name: mustName("evil.example"), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}},
			wantErr: errResponseQuestionMismatch,
		},
		{
			name:    "another type",
			options: responseOptions{id: 7, question: dnsmessage.Question{Name: queried, Type: dnsmessage.TypeAAAA, Class: dnsmessage.ClassINET}},
			wantErr: errResponseQuestionMismatch,
		},
		{name: "no question", options: responseOptions{id: 7, skipQuestions: true}, wantErr: errResponseQuestionMismatch},
		{name: "server failure", options: responseOptions{id: 7, rcode: dnsmessage.RCodeServerFailure, question: question}, wantAnyErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readAnswer(buildResponse(t, tc.options), 7, queried, dnsmessage.TypeA)
			if tc.wantErr != nil || tc.wantAnyErr {
				if err == nil || (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.negative != tc.wantNegative || got.ttl != tc.wantTTL {
				t.Fatalf("negative %v ttl %v, want %v %v", got.negative, got.ttl, tc.wantNegative, tc.wantTTL)
			}
			if len(got.addresses) != len(tc.wantAddresses) {
				t.Fatalf("addresses %v, want %v", got.addresses, tc.wantAddresses)
			}
			for index, want := range tc.wantAddresses {
				if got.addresses[index] != netip.MustParseAddr(want) {
					t.Fatalf("addresses %v, want %v", got.addresses, tc.wantAddresses)
				}
			}
		})
	}
}

func TestReadAnswerRefusesALongCNAMEChain(t *testing.T) {
	queried := mustName(hopName(0))
	var records []dnsmessage.Resource
	for hop := range maxCNAMEChainLength + 2 {
		records = append(records, cnameRecord(hopName(hop), 60, hopName(hop+1)))
	}
	response := buildResponse(t, responseOptions{id: 1, question: dnsmessage.Question{Name: queried, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, answers: records})
	if _, err := readAnswer(response, 1, queried, dnsmessage.TypeA); !errors.Is(err, errCNAMEChainTooLong) {
		t.Fatalf("err = %v, want %v", err, errCNAMEChainTooLong)
	}
}

func TestReadAnswerSurvivesACNAMELoop(t *testing.T) {
	queried := mustName("a.example")
	response := buildResponse(t, responseOptions{id: 1, question: dnsmessage.Question{Name: queried, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
		answers: []dnsmessage.Resource{cnameRecord("a.example", 60, "b.example"), cnameRecord("b.example", 60, "a.example")}})
	if _, err := readAnswer(response, 1, queried, dnsmessage.TypeA); !errors.Is(err, errCNAMEChainTooLong) {
		t.Fatalf("err = %v, want %v", err, errCNAMEChainTooLong)
	}
}

func hopName(hop int) string { return "hop" + string(rune('a'+hop)) + ".example" }

// FuzzReadAnswer: whatever bytes arrive, reading them never panics, and an
// answer it accepts has addresses or is negative.
func FuzzReadAnswer(f *testing.F) {
	queried := mustName("www.example.com")
	question := dnsmessage.Question{Name: queried, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}
	t := &testing.T{}
	f.Add(buildResponse(t, responseOptions{id: 7, question: question, answers: []dnsmessage.Resource{aRecord("www.example.com", 300, "192.0.2.1")}}))
	f.Add(buildResponse(t, responseOptions{id: 7, rcode: dnsmessage.RCodeNameError, question: question, authorities: []dnsmessage.Resource{soaRecord(90, 45)}}))
	f.Add(buildResponse(t, responseOptions{id: 7, question: question, answers: []dnsmessage.Resource{cnameRecord("www.example.com", 30, "a.example"), aRecord("a.example", 1, "192.0.2.3")}}))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, response []byte) {
		got, err := readAnswer(response, 7, queried, dnsmessage.TypeA)
		if err == nil && !got.negative && len(got.addresses) == 0 {
			t.Fatal("accepted an answer with neither addresses nor a negative result")
		}
		if got.negative && got.ttl > maxNegativeAnswerTTL {
			t.Fatalf("negative TTL %v above the cap", got.ttl)
		}
	})
}

func TestParseLookupName(t *testing.T) {
	for _, valid := range []string{"example.com", "Example.COM.", "_dmarc.example.com", "xn--mgbh0fb.xn--kgbechtv", "a"} {
		if _, err := parseLookupName(valid); err != nil {
			t.Errorf("%q: %v", valid, err)
		}
	}
	long := ""
	for range 64 {
		long += "a"
	}
	for _, invalid := range []string{"", ".", "a..b", "exa mple.com", "exam\x00ple.com", "ایران.ir", "a/b.com", long + ".com", "*.example.com"} {
		if _, err := parseLookupName(invalid); err == nil {
			t.Errorf("%q accepted", invalid)
		}
	}
}
