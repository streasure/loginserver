package service

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ACL prometheus 指标（默认注册表，经 upprof /metrics 暴露）
var (
	aclCheckTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loginserver",
		Subsystem: "acl",
		Name:      "check_total",
		Help:      "ACL 检测次数（kind=ip|role，result=pass|white|block|error）",
	}, []string{"kind", "result"})

	aclSnapshotReloadTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loginserver",
		Subsystem: "acl",
		Name:      "snapshot_reload_total",
		Help:      "ACL 快照全量重载次数（result=success|error）",
	}, []string{"result"})

	aclSnapshotReloadSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loginserver",
		Subsystem: "acl",
		Name:      "snapshot_reload_seconds",
		Help:      "ACL 快照全量重载耗时",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	aclSnapshotSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "loginserver",
		Subsystem: "acl",
		Name:      "snapshot_size",
		Help:      "ACL 快照条目数（kind=white|black|role）",
	}, []string{"kind"})
)

func recordAclCheck(kind, result string) {
	aclCheckTotal.WithLabelValues(kind, result).Inc()
}

func recordAclSnapshotReload(result string, d time.Duration) {
	aclSnapshotReloadTotal.WithLabelValues(result).Inc()
	if result == "success" {
		aclSnapshotReloadSeconds.Observe(d.Seconds())
	}
}

func updateAclSnapshotGauges(d *aclData) {
	aclSnapshotSize.WithLabelValues("white").Set(float64(len(d.white)))
	aclSnapshotSize.WithLabelValues("black").Set(float64(len(d.black)))
	aclSnapshotSize.WithLabelValues("role").Set(float64(len(d.roles)))
}
