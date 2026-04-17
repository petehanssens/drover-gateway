import FullPageLoader from "@/components/fullPageLoader";
import Header from "@/components/header";
import NotAvailableBanner from "@/components/notAvailableBanner";
import ProgressProvider from "@/components/progressBar";
import Sidebar from "@/components/sidebar";
import { ThemeProvider } from "@/components/themeProvider";
import { SidebarProvider } from "@/components/ui/sidebar";
import { useStoreSync } from "@/hooks/useStoreSync";
import { WebSocketProvider } from "@/hooks/useWebSocket";
import { getErrorMessage, ReduxProvider, useGetCoreConfigQuery } from "@/lib/store";
import { BifrostConfig } from "@/lib/types/config";
import { RbacProvider } from "@enterprise/lib/contexts/rbacContext";
import { useLocation } from "@tanstack/react-router";
import { NuqsAdapter } from "nuqs/adapters/tanstack-router";
import { Suspense, lazy, useEffect } from "react";
import { CookiesProvider } from "react-cookie";
import { toast, Toaster } from "sonner";

// Lazy import — only loaded in development, completely excluded from prod bundle
const DevProfilerLazy = lazy(() => import("@/components/devProfiler").then((mod) => ({ default: mod.DevProfiler })));
const DevProfiler = () => (
	<Suspense fallback={null}>
		<DevProfilerLazy />
	</Suspense>
);

function StoreSyncInitializer() {
	useStoreSync();
	return null;
}

function AppContent({ children }: { children: React.ReactNode }) {
	const { data: bifrostConfig, error, isLoading } = useGetCoreConfigQuery({});

	useEffect(() => {
		if (error) {
			toast.error(getErrorMessage(error));
		}
	}, [error]);

	return (
		<WebSocketProvider>
			<CookiesProvider>
				<StoreSyncInitializer />
				<SidebarProvider>
					<Sidebar />
					<div className="dark:bg-card custom-scrollbar content-container h-[100dvh] w-full overflow-auto bg-white md:my-[0.5rem] md:mr-[0.5rem] md:h-[calc(100dvh-1rem)] md:min-w-xl md:rounded-md md:border md:border-gray-200 md:px-10 dark:border-zinc-800 dark:md:border-zinc-800">
						<Header />
						<main className="custom-scrollbar content-container-inner relative mx-auto flex flex-col overflow-y-hidden p-3 md:p-4">
							{isLoading ? <FullPageLoader /> : <FullPage config={bifrostConfig}>{children}</FullPage>}
						</main>
					</div>
				</SidebarProvider>
			</CookiesProvider>
		</WebSocketProvider>
	);
}

function FullPage({ config, children }: { config: BifrostConfig | undefined; children: React.ReactNode }) {
	const pathname = useLocation({ select: (l) => l.pathname });
	if (config && config.is_db_connected) {
		return children;
	}
	if (config && config.is_logs_connected && pathname.startsWith("/workspace/logs")) {
		return children;
	}
	return <NotAvailableBanner />;
}

export function ClientLayout({ children }: { children: React.ReactNode }) {
	return (
		<ProgressProvider>
			<ThemeProvider attribute="class" defaultTheme="system" enableSystem>
				<Toaster />
				<ReduxProvider>
					<NuqsAdapter>
						<RbacProvider>
							<AppContent>{children}</AppContent>
							{process.env.NODE_ENV === "development" && !process.env.BIFROST_DISABLE_PROFILER && <DevProfiler />}
						</RbacProvider>
					</NuqsAdapter>
				</ReduxProvider>
			</ThemeProvider>
		</ProgressProvider>
	);
}