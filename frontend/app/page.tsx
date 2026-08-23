import { OverviewPanel } from "@/components/monitor/overview-panel"
import { ChannelCards } from "@/components/monitor/channel-cards"
import { BottomPanels } from "@/components/monitor/bottom-panels"
import { DispatchHealthPanel } from "@/components/monitor/dispatch-health-panel"

export default function Page() {
  return (
    <>
      <OverviewPanel />

      <DispatchHealthPanel />

      <ChannelCards />

      <BottomPanels />
    </>
  )
}
