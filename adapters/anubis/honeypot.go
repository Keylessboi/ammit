package anubis

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/TecharoHQ/anubis/lib/config"
	"github.com/TecharoHQ/anubis/lib/policy/checker"
	"github.com/TecharoHQ/anubis/lib/store"

	"github.com/Keylessboi/ammit/pkg/httpsrv"
)

// Honeypot is the Tier-2 integration: an implementation of the honeypot
// subsystem that Anubis dispatches to at /honeypot/{id}/{stage}.
//
// It has the same shape as Anubis's own naive implementation on purpose, so that
// wiring it in is a change to config.Honeypot.Valid and one line in the
// dispatch. Where naive generates spintax nonsense whose only purpose is to
// waste a crawler's time, this generates a corpus.
type Honeypot struct {
	st            store.Interface
	networkWeight store.JSON[int]
	lg            *slog.Logger
	srv           *httpsrv.Server
}

// errNoServer is returned when Ammit was asked to act as the honeypot but no
// configuration is available to generate content from.
var errNoServer = errNoServerError{}

type errNoServerError struct{}

func (errNoServerError) Error() string {
	return "ammit: honeypot requested but no configuration found; set " + EnvConfig
}

// NewHoneypot builds the Tier-2 honeypot.
//
// The signature deliberately matches naive.New so that Anubis's dispatch site
// reads the same for both implementations.
func NewHoneypot(cfg *config.Honeypot, st store.Interface, lg *slog.Logger) (*Honeypot, error) {
	e := &Extension{}
	srv := e.Server()
	if err := e.Err(); err != nil {
		return nil, err
	}
	if srv == nil {
		return nil, errNoServer
	}

	return &Honeypot{
		st:            st,
		networkWeight: store.JSON[int]{Underlying: st, Prefix: "ammit:network"},
		lg:            lg.With("component", "honeypot/ammit"),
		srv:           srv,
	}, nil
}

// ServeHTTP renders a trap page for the matched route.
//
// Anubis's mux has already extracted {id} and {stage} by the time this runs, so
// it delegates to the shared renderer rather than re-deriving anything.
func (h *Honeypot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.srv.ServePage(w, r)
}

// CheckNetwork returns a rule that fires once a network has pulled enough pages.
//
// The count is per clamped network rather than per address, because a crawler
// that rotates addresses within a subnet is still one operator, and per-address
// counting is trivially defeated by a proxy pool.
func (h *Honeypot) CheckNetwork() checker.Impl {
	return checker.Func(func(r *http.Request) (bool, error) {
		addr, ok := realIP(r)
		if !ok {
			return false, nil
		}
		network, ok := clampIP(addr)
		if !ok {
			return false, nil
		}

		key := storeKey(network.String())
		count, _ := h.networkWeight.Get(r.Context(), key)
		if count >= crawlerThreshold {
			return true, nil
		}
		return false, nil
	})
}

// Hash identifies the implementation for cache-busting purposes.
func (h *Honeypot) Hash() string {
	return "ammit honeypot v1"
}

// crawlerThreshold is how many pages one network must pull before it is treated
// as a crawler rather than a person. It matches the shipped naive implementation
// so that swapping implementations does not change when the rule fires.
const crawlerThreshold = 25

// Record increments the network counter. Exported so a host can call it on the
// request path where the counting belongs, rather than inside a checker that may
// be invoked several times per request.
func (h *Honeypot) Record(ctx *http.Request) {
	addr, ok := realIP(ctx)
	if !ok {
		return
	}
	network, ok := clampIP(addr)
	if !ok {
		return
	}

	key := storeKey(network.String())
	count, _ := h.networkWeight.Get(ctx.Context(), key)
	if err := h.networkWeight.Set(ctx.Context(), key, count+1, time.Hour); err != nil {
		h.lg.Warn("ammit: could not record network hit", "err", err)
	}
}

// realIP extracts the client address, preferring X-Real-IP when the direct peer
// is not usable.
func realIP(r *http.Request) (netip.Addr, bool) {
	if addrPort, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		return addrPort.Addr().Unmap(), true
	}
	if addr, err := netip.ParseAddr(r.RemoteAddr); err == nil {
		return addr.Unmap(), true
	}
	if addr, err := netip.ParseAddr(r.Header.Get("X-Real-IP")); err == nil {
		return addr.Unmap(), true
	}
	return netip.Addr{}, false
}

// clampIP reduces an address to the network a counter should be kept for.
//
// This duplicates a helper inside Anubis's own internal package. It is
// duplicated rather than imported because Go forbids a third-party module from
// importing another module's internal packages, and the logic is four lines.
func clampIP(addr netip.Addr) (netip.Prefix, bool) {
	if !addr.IsValid() {
		return netip.Prefix{}, false
	}
	if addr.Is4() {
		return netip.PrefixFrom(addr, 24), true
	}
	if addr.Is6() {
		// A /64 is the smallest allocation normally handed to one subscriber,
		// so it is the right granularity for one crawler operator.
		return netip.PrefixFrom(addr, 64), true
	}
	return netip.Prefix{}, false
}

// storeKey hashes a network string for use as a store key.
//
// The raw network is not used as the key directly because store keys end up in
// backend paths and metrics labels, and an address in a label is a privacy leak
// in any log or dashboard that scrapes it.
func storeKey(s string) string {
	sum := sha256.Sum256([]byte("ammit/network/v1|" + s))
	return hex.EncodeToString(sum[:12])
}
