package senders

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxyStress(t *testing.T) {
	// proxy := os.Getenv("WF_PROXY")
	proxy := "10.40.62.212"
	if proxy == "" {
		t.Skip("No WF_PROXY specified. Skipping")
		return
	}
	s, err := NewProxySender(&ProxyConfiguration{
		Host:        proxy,
		MetricsPort: 2878,
	})
	require.NoError(t, err)

	// Generate a nasty amount of data
	n := 100000
	m := make(map[string]string)
	for i := 0; i < 20; i++ {
		m["k"+strconv.Itoa(i)] = "v" + strconv.Itoa(i)
	}
	for i := 0; i < n; i++ {
		require.NoError(t, s.SendMetric("junk.garbage", 0, time.Now().UnixNano(), "some_source_%d", m))
	}
	require.NoError(t, s.Flush())
	s.Close()
}
