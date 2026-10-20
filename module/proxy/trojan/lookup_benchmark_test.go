package trojan

import (
	"fmt"
	"testing"

	"github.com/we-apranik/apranik/module/proxy/trojan/wire"
)

func BenchmarkLookUp(b *testing.B) {
	for _, count := range []int{1, 100, 10000} {
		b.Run(fmt.Sprintf("%d users", count), func(b *testing.B) {
			users := make([]user, count)
			for index := range users {
				users[index] = user{name: fmt.Sprintf("user-%d", index), passwordHash: wire.HashPassword(fmt.Sprintf("password-%d", index))}
			}
			server := newServer(users, 0)
			wanted := users[count-1].passwordHash

			b.ReportAllocs()
			for b.Loop() {
				if _, found := server.lookUp(wanted); !found {
					b.Fatal("the last user was not found")
				}
			}
		})
	}
}
