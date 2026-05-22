package ctxkey

type key string

const (
	// CtxRunID 用于在 context 中传递当前执行的 run ID。
	CtxRunID key = "pulseops:runID"
	// CtxTriggerRun 携带触发当前任务的源 RunRecord。
	// 由 runtime.Manager 在执行依赖任务时注入 context。
	CtxTriggerRun key = "pulseops:triggerRun"
)
