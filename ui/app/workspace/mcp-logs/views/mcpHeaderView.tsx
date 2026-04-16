import { MCPFilterSidebar } from "@/components/filters/mcpFilterSidebar";
import { ColumnConfigDropdown, type ColumnConfigEntry } from "@/components/table";
import { Button } from "@/components/ui/button";
import { DateTimePickerWithRange } from "@/components/ui/datePickerWithRange";
import { Input } from "@/components/ui/input";
import { Sheet, SheetContent, SheetDescription, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import type { MCPToolLogFilters } from "@/lib/types/logs";
import { Filter, Pause, Play, Search } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";

const LOG_TIME_PERIODS = [
	{ label: "Last hour", value: "1h" },
	{ label: "Last 6 hours", value: "6h" },
	{ label: "Last 24 hours", value: "24h" },
	{ label: "Last 7 days", value: "7d" },
	{ label: "Last 30 days", value: "30d" },
];

function getRangeForPeriod(period: string): { from: Date; to: Date } {
	const to = new Date();
	const from = new Date(to.getTime());
	switch (period) {
		case "1h":
			from.setHours(from.getHours() - 1);
			break;
		case "6h":
			from.setHours(from.getHours() - 6);
			break;
		case "24h":
			from.setHours(from.getHours() - 24);
			break;
		case "7d":
			from.setDate(from.getDate() - 7);
			break;
		case "30d":
			from.setDate(from.getDate() - 30);
			break;
		default:
			from.setHours(from.getHours() - 24);
	}
	return { from, to };
}

interface McpHeaderViewProps {
	filters: MCPToolLogFilters;
	onFiltersChange: (filters: MCPToolLogFilters) => void;
	liveEnabled: boolean;
	onLiveToggle: (enabled: boolean) => void;
	/** Column config for the ColumnConfigDropdown */
	columnEntries: ColumnConfigEntry[];
	columnLabels: Record<string, string>;
	onToggleColumnVisibility: (id: string) => void;
	onResetColumns: () => void;
}

export function McpHeaderView({
	filters,
	onFiltersChange,
	liveEnabled,
	onLiveToggle,
	columnEntries,
	columnLabels,
	onToggleColumnVisibility,
	onResetColumns,
}: McpHeaderViewProps) {
	const [localSearch, setLocalSearch] = useState(filters.content_search || "");
	const [startTime, setStartTime] = useState<Date | undefined>(filters.start_time ? new Date(filters.start_time) : undefined);
	const [endTime, setEndTime] = useState<Date | undefined>(filters.end_time ? new Date(filters.end_time) : undefined);
	const searchTimeoutRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
	const filtersRef = useRef<MCPToolLogFilters>(filters);

	// Keep filtersRef in sync with filters prop
	useEffect(() => {
		filtersRef.current = filters;
	}, [filters]);

	// Sync localSearch when filters.content_search changes externally
	useEffect(() => {
		setLocalSearch(filters.content_search || "");
	}, [filters.content_search]);

	useEffect(() => {
		setStartTime(filters.start_time ? new Date(filters.start_time) : undefined);
		setEndTime(filters.end_time ? new Date(filters.end_time) : undefined);
	}, [filters.start_time, filters.end_time]);

	// Cleanup timeout on unmount
	useEffect(() => {
		return () => {
			if (searchTimeoutRef.current) {
				clearTimeout(searchTimeoutRef.current);
			}
		};
	}, []);

	const handleSearchChange = useCallback(
		(value: string) => {
			setLocalSearch(value);

			if (searchTimeoutRef.current) {
				clearTimeout(searchTimeoutRef.current);
			}

			searchTimeoutRef.current = setTimeout(() => {
				onFiltersChange({ ...filtersRef.current, content_search: value });
			}, 500);
		},
		[onFiltersChange],
	);

	return (
		<div className="flex grow flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
			{/* Mobile row 1: filter + live on the left, column-config right-aligned.
			    On sm+ this wrapper becomes display:contents so its children flow inline with the rest. */}
			<div className="flex items-center gap-2 sm:contents">
				{/* Filter sheet trigger — mobile only */}
				<Sheet>
					<SheetTrigger asChild>
						<Button variant="outline" size="sm" className="h-7.5 shrink-0 md:hidden" aria-label="Open filters">
							<Filter className="size-4" />
						</Button>
					</SheetTrigger>
					<SheetContent side="left" className="w-[85vw] max-w-sm border-r p-0">
						<SheetTitle className="sr-only">Filters</SheetTitle>
						<SheetDescription className="sr-only">Filter MCP logs by tool, server, status, and virtual key.</SheetDescription>
						<MCPFilterSidebar filters={filters} onFiltersChange={onFiltersChange} disableCollapse className="w-full rounded-none" />
					</SheetContent>
				</Sheet>

				<Button variant={"outline"} size="sm" className="h-7.5 shrink-0 sm:order-1" onClick={() => onLiveToggle(!liveEnabled)}>
					{liveEnabled ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
					<span className="hidden sm:inline">Live updates</span>
				</Button>

				<div className="ml-auto flex items-center gap-2 sm:contents">
					<div className="sm:order-4">
						<ColumnConfigDropdown
							entries={columnEntries}
							labels={columnLabels}
							onToggleVisibility={onToggleColumnVisibility}
							onReset={onResetColumns}
						/>
					</div>
				</div>
			</div>

			{/* Search — mobile row 2 (full width); on desktop, slides between Live and Date as flex-1 */}
			<div className="border-input flex h-7.5 w-full items-center gap-2 rounded-sm border sm:w-auto sm:flex-1 sm:order-2">
				<Search className="mr-0.5 ml-2 size-4 shrink-0" />
				<Input
					type="text"
					className="!h-7 rounded-tl-none rounded-tr-sm rounded-br-sm rounded-bl-none border-none bg-slate-50 shadow-none outline-none focus-visible:ring-0 dark:bg-zinc-900"
					placeholder="Search MCP logs"
					value={localSearch}
					onChange={(e) => handleSearchChange(e.target.value)}
				/>
			</div>

			{/* Date picker — mobile row 3 (full width); auto width on desktop */}
			<DateTimePickerWithRange
				className="w-full sm:w-auto sm:order-3"
				buttonClassName="w-full justify-start sm:w-auto"
				dateTime={{ from: startTime, to: endTime }}
				onDateTimeUpdate={(p) => {
					setStartTime(p.from);
					setEndTime(p.to);
					onFiltersChange({
						...filters,
						start_time: p.from?.toISOString(),
						end_time: p.to?.toISOString(),
					});
				}}
				preDefinedPeriods={LOG_TIME_PERIODS}
				onPredefinedPeriodChange={(periodValue) => {
					if (!periodValue) return;
					const { from, to } = getRangeForPeriod(periodValue);
					setStartTime(from);
					setEndTime(to);
					onFiltersChange({
						...filters,
						start_time: from.toISOString(),
						end_time: to.toISOString(),
					});
				}}
			/>
		</div>
	);
}
