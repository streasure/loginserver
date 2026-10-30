package service

import (
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

func updateAclSnapshotGauges(d *aclData) {
	aclSnapshotSize.WithLabelValues("white").Set(float64(len(d.white)))
	aclSnapshotSize.WithLabelValues("black").Set(float64(len(d.black)))
	aclSnapshotSize.WithLabelValues("role").Set(float64(len(d.roles)))
}
