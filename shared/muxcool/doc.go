// Package muxcool is Mux.Cool, the multiplexing format of V2Ray and Xray: many
// independent sub-connections carried over one established reliable stream, the
// main connection. VMess, VLESS and Shadowsocks can enter it (VLESS with
// command 3, the others with the destination "v1.mux.cool"); this package knows
package muxcool
