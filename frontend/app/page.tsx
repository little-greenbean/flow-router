import { KpiRow } from "@/components/monitor/kpi-row"
import { BalanceOverview } from "@/components/monitor/balance-overview"
import { MultiplierChanges } from "@/components/monitor/multiplier-changes"
import { ChannelCards } from "@/components/monitor/channel-cards"
import { BottomPanels } from "@/components/monitor/bottom-panels"
import { DispatchHealthPanel } from "@/components/monitor/dispatch-health-panel"

export default function Page() {
  return (
    <>
      <KpiRow />

      <DispatchHealthPanel />

      <div className="grid grid-cols-1 gap-3 lg:grid-cols-5">
        <div className="lg:col-span-3">
          <BalanceOverview />
        </div>
        <div className="lg:col-span-2">
          <MultiplierChanges />
        </div>
      </div>

      <ChannelCards />

      <BottomPanels />
    </>
  )
}
