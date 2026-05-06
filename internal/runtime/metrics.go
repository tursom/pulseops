package runtime

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	tasksLoaded      prometheus.Gauge
	taskRunsTotal    *prometheus.CounterVec
	taskLastDuration *prometheus.GaugeVec
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		tasksLoaded: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "pulseops_tasks_loaded",
			Help: "Current number of loaded tasks.",
		}),
		taskRunsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "pulseops_task_runs_total",
			Help: "Total task runs grouped by task and result.",
		}, []string{"task_id", "kind", "run_status", "check_status"}),
		taskLastDuration: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "pulseops_task_last_duration_ms",
			Help: "Last task execution duration in milliseconds.",
		}, []string{"task_id", "kind"}),
	}
	prometheus.MustRegister(metrics.tasksLoaded, metrics.taskRunsTotal, metrics.taskLastDuration)
	return metrics
}
