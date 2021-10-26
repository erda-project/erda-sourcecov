package core

const Running = "running"
const Ready = "ready"
const Success = "success"
const Fail = "fail"
const Error = "error"

type CodeCoverageExecStatus string

const (
	RunningStatus CodeCoverageExecStatus = "running"
	ReadyStatus   CodeCoverageExecStatus = "ready"
	EndingStatus  CodeCoverageExecStatus = "ending"
	CancelStatus  CodeCoverageExecStatus = "cancel"
	SuccessStatus CodeCoverageExecStatus = "success"
	FailStatus    CodeCoverageExecStatus = "fail"
)
