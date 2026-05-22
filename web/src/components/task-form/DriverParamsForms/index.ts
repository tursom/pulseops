import type { FC } from 'react'
import HTTPCheckParams from './HTTPCheckParams'
import TCPCheckParams from './TCPCheckParams'
import ScriptExecParams from './ScriptExecParams'
import ProcessCheckParams from './ProcessCheckParams'
import ScenarioCheckParams from './ScenarioCheckParams'
import AIAnalyzeParams from './AIAnalyzeParams'
import UpstreamDataParams from './UpstreamDataParams'

export const driverForms: Record<string, FC> = {
  http_check: HTTPCheckParams,
  tcp_check: TCPCheckParams,
  script_exec: ScriptExecParams,
  process_check: ProcessCheckParams,
  scenario_check: ScenarioCheckParams,
  ai_analyze: AIAnalyzeParams,
  data_process: UpstreamDataParams,
}
