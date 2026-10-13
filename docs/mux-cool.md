# Mux.Cool on the wire

Mux.Cool carries many sub-connections over one established stream, the main connection. It is implemented in `shared/muxcool`
. All numbers are big endian.

## Frame

| Part | Size | Meaning |
| --- | --- | --- |
| LENGTH | 2 | Length of the metadata |
| METADATA | LENGTH | ID, status, options and extra fields, below |
| DATA-LENGTH | 2 | Only when the options say data follows |
| DATA | DATA-LENGTH | The payload |

## Metadata

| Part | Size | Meaning |
| --- | --- | --- |
| ID | 2 | The sub-connection |
| STATUS | 1 | 1 New, 2 Keep, 3 End, 4 KeepAlive |
| OPTIONS | 1 | Bit 0: data follows. Bit 1: error (on End) |
| EXTRA | | By status, below |

| Status | EXTRA |
| --- | --- |
| New | NETWORK(1: 1 TCP, 2 UDP), PORT(2), TYPE(1: 1 IPv4, 2 domain, 3 IPv6), ADDRESS. A UDP New with data may add GLOBAL ID(8) (XUDP) |
| Keep | Nothing. For UDP it may be NETWORK(1) = 2, PORT(2), TYPE(1), ADDRESS (XUDP) |
| End, KeepAlive | Nothing |

A domain ADDRESS is a length byte and then the name. The port-first layout is VMess's; SOCKS5 and Trojan use another.

## Flows

| Form | Client sends | Server sends |
| --- | --- | --- |
| TCP | New (network, destination); Keep + data; End | Keep + data; End |
| UDP | New (network, destination) + first datagram; Keep + datagram | Keep + datagram, no address |
| XUDP | New (UDP, first destination) + datagram; Keep (UDP, destination) + datagram | Keep (UDP, source) + datagram, once the client has named an address |

VLESS enters Mux.Cool with command 3, and Apranik uses it for XUDP only. A server never sends New. A refused New is answered with an End carrying the error bit, and the main connection stays. A New for
an ID that is open closes the main connection.
