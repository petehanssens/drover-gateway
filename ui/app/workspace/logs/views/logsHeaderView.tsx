import { LogsFilterSidebar } from "@/components/filters/logsFilterSidebar";
import { ColumnConfigDropdown, type ColumnConfigEntry } from "@/components/table";
import { Button } from "@/components/ui/button";
import { Command, CommandItem, CommandList } from "@/components/ui/command";
import { DateTimePickerWithRange } from "@/components/ui/datePickerWithRange";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Sheet, SheetContent, SheetDescription, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { getErrorMessage, useRecalculateLogCostsMutation } from "@/lib/store";
import type { LogFilters as LogFiltersType } from "@/lib/types/logs";
import { Calculator, Filter, MoreVertical, Pause, Play, Search } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

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

interface LogsHeaderViewProps {
	filters: LogFiltersType;
	onFiltersChange: (filters: LogFiltersType) => void;
	liveEnabled: boolean;
	onLiveToggle: (enabled: boolean) => void;
	fetchLogs: () => Promise<void>;
	fetchStats: () => Promise<void>;
	/** Column config for the ColumnConfigDropdown */
	columnEntries: ColumnConfigEntry[];
	columnLabels: Record<string, string>;
	onToggleColumnVisibility: (id: string) => void;
	onResetColumns: () => void;
}

export function LogsHeaderView({
	filters,
	onFiltersChange,
	liveEnabled,
	onLiveToggle,
	fetchLogs,
	fetchStats,
	columnEntries,
	columnLabels,
	onToggleColumnVisibility,
	onResetColumns,
}: LogsHeaderViewProps) {
	const [openMoreActionsPopover, setOpenMoreActionsPopover] = useState(false);
	const [localSearch, setLocalSearch] = useState(filters.content_search || "");
	const searchTimeoutRef = useRef<NodeJS.Timeout | undefined>(undefined);
	const filtersRef = useRef<LogFiltersType>(filters);
	const [recalculateCosts] = useRecalculateLogCostsMutation();

	const [startTime, setStartTime] = useState<Date | undefined>(filters.start_time ? new Date(filters.start_time) : undefined);
	const [endTime, setEndTime] = useState<Date | undefined>(filters.end_time ? new Date(filters.end_time) : undefined);

	useEffect(() => {
		setStartTime(filters.start_time ? new Date(filters.start_time) : undefined);
		setEndTime(filters.end_time ? new Date(filters.end_time) : undefined);
	}, [filters.start_time, filters.end_time]);

	// Keep filtersRef in sync so debounced search always merges with latest filters (search within filtered results)
	useEffect(() => {
		filtersRef.current = filters;
	}, [filters]);

	// Sync localSearch when filters.content_search changes externally (e.g. URL restore)
	useEffect(() => {
		setLocalSearch(filters.content_search || "");
	}, [filters.content_search]);

	// Cleanup timeout on unmount
	useEffect(() => {
		return () => {
			if (searchTimeoutRef.current) {
				clearTimeout(searchTimeoutRef.current);
			}
		};
	}, []);

	const handleRecalculateCosts = useCallback(async () => {
		try {
			const response = await recalculateCosts({ filters }).unwrap();
			await fetchLogs();
			await fetchStats();
			setOpenMoreActionsPopover(false);
			toast.success(`Recalculated costs for ${response.updated} logs`, {
				description: `${response.updated} logs updated, ${response.skipped} logs skipped, ${response.remaining} logs remaining`,
				duration: 5000,
			});
		} catch (err) {
			toast.error(getErrorMessage(err));
		}
	}, [filters, recalculateCosts, fetchLogs, fetchStats]);

	const handleSearchChange = useCallback(
		(value: string) => {
			setLocalSearch(value);

			if (searchTimeoutRef.current) {
				clearTimeout(searchTimeoutRef.current);
			}

			// Use filtersRef.current so search is applied on top of current filters (search within filtered results)
			searchTimeoutRef.current = setTimeout(() => {
				onFiltersChange({ ...filtersRef.current, content_search: value });
			}, 500);
		},
		[onFiltersChange],
	);

	return (
		<div className="flex grow flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
			{/* Mobile row 1: icon buttons (filter, live) on the left + (more, columns) right-aligned.
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
						<SheetDescription className="sr-only">Filter logs by status, providers, models, and metadata.</SheetDescription>
						<LogsFilterSidebar filters={filters} onFiltersChange={onFiltersChange} disableCollapse className="w-full rounded-none" />
					</SheetContent>
				</Sheet>

				<Button variant={"outline"} size="sm" className="h-7.5 shrink-0 sm:order-1" onClick={() => onLiveToggle(!liveEnabled)}>
					{liveEnabled ? <Pause className="h-4 w-4" /> : <Play className="h-4 w-4" />}
					<span className="hidden sm:inline">Live updates</span>
				</Button>

				<div className="ml-auto flex items-center gap-2 sm:contents">
					<Popover open={openMoreActionsPopover} onOpenChange={setOpenMoreActionsPopover}>
						<PopoverTrigger asChild>
							<Button variant="outline" size="sm" className="h-7.5 w-7.5 sm:order-4">
								<MoreVertical className="h-4 w-4" />
							</Button>
						</PopoverTrigger>
						<PopoverContent className="bg-accent w-[250px] p-2" align="end">
							<Command>
								<CommandList>
									<CommandItem className="hover:bg-accent/50 cursor-pointer" onSelect={handleRecalculateCosts}>
										<Calculator className="text-muted-foreground size-4" />
										<div className="flex flex-col">
											<span className="text-sm">Recalculate costs</span>
											<span className="text-muted-foreground text-xs">For all logs that don't have a cost</span>
										</div>
									</CommandItem>
								</CommandList>
							</Command>
						</PopoverContent>
					</Popover>
					<div className="sm:order-5">
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
			<div className="border-input flex h-7.5 w-full items-center gap-2 rounded-sm border sm:order-2 sm:w-auto sm:flex-1">
				<Search className="mr-0.5 ml-2 size-4 shrink-0" />
				<Input
					type="text"
					className="!h-7 rounded-tl-none rounded-tr-sm rounded-br-sm rounded-bl-none border-none bg-slate-50 shadow-none outline-none focus-visible:ring-0"
					placeholder="Search logs"
					value={localSearch}
					onChange={(e) => handleSearchChange(e.target.value)}
				/>
			</div>

			{/* Date picker — mobile row 3 (full width); auto width on desktop */}
			<DateTimePickerWithRange
				triggerTestId="filter-date-range"
				className="w-full sm:order-3 sm:w-auto"
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