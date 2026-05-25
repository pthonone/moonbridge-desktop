// Direct Wails bindings wrapper - bypasses Vite tree-shaking
// by accessing window.go directly at runtime instead of ESM imports.

const go = (window as any).go?.main?.App

function assertGo() {
  if (!go) throw new Error('Wails Go bindings not available. window.go.main.App is missing.')
}

export const GetStatus          = () => { assertGo(); return go.GetStatus() }
export const StartProxy         = () => { assertGo(); return go.StartProxy() }
export const StopProxy          = () => { assertGo(); return go.StopProxy() }
export const GetConfig          = () => { assertGo(); return go.GetConfig() }
export const SaveConfig         = (a1: any) => { assertGo(); return go.SaveConfig(a1) }
export const ListModels         = () => { assertGo(); return go.ListModels() }
export const ListProviders      = () => { assertGo(); return go.ListProviders() }
export const AddProvider        = (a1: any) => { assertGo(); return go.AddProvider(a1) }
export const UpdateProvider     = (a1: any, a2: any) => { assertGo(); return go.UpdateProvider(a1, a2) }
export const DeleteProvider     = (a1: any) => { assertGo(); return go.DeleteProvider(a1) }
export const SwitchModel        = (a1: any) => { assertGo(); return go.SwitchModel(a1) }
export const GetUsageStats      = () => { assertGo(); return go.GetUsageStats() }
export const GetProviderPresets = () => { assertGo(); return go.GetProviderPresets() }
export const SetPort            = (a1: any) => { assertGo(); return go.SetPort(a1) }
export const SetLogLevel        = (a1: any) => { assertGo(); return go.SetLogLevel(a1) }
export const SyncCodexConfig    = () => { assertGo(); return go.SyncCodexConfig() }
export const IsCodexInstalled   = () => { assertGo(); return go.IsCodexInstalled() }
export const GetUsageDailyStats = (a1: any) => { assertGo(); return go.GetUsageDailyStats(a1) }
export const GetUsageRecentRecords = (a1: any) => { assertGo(); return go.GetUsageRecentRecords(a1) }
export const GetUsageByModel    = (a1: any, a2: any) => { assertGo(); return go.GetUsageByModel(a1, a2) }
export const ClearUsageToday    = () => { assertGo(); return go.ClearUsageToday() }
export const ClearUsageAll      = () => { assertGo(); return go.ClearUsageAll() }
export const AddUsage           = (a1: any, a2: any, a3: any, a4: any, a5: any) => { assertGo(); return go.AddUsage(a1, a2, a3, a4, a5) }
export const ClearUsageDateRange = (a1: any, a2: any) => { assertGo(); return go.ClearUsageDateRange(a1, a2) }
export const IsCodexEnabled     = () => { assertGo(); return go.IsCodexEnabled() }
export const SetCodexEnabled    = (a1: any) => { assertGo(); return go.SetCodexEnabled(a1) }
