import { useSidebar } from "@/components/ui/sidebar";
import { Link } from "@tanstack/react-router";
import { Menu, X } from "lucide-react";
import { useTheme } from "next-themes";
import { useEffect, useState } from "react";

export default function Header() {
	const { toggleSidebar, openMobile } = useSidebar();
	const { resolvedTheme } = useTheme();
	const [mounted, setMounted] = useState(false);

	useEffect(() => {
		setMounted(true);
	}, []);

	const logoSrc = mounted && resolvedTheme === "dark" ? "/bifrost-logo-dark.webp" : "/bifrost-logo.webp";

	return (
		<header className="bg-background sticky top-0 z-[60] flex h-12 items-center justify-between border-b px-3 md:hidden">
			<Link to="/workspace/logs" className="flex items-center pl-1">
				<img src={logoSrc} alt="Bifrost" className="h-[22px] w-auto" />
			</Link>
			<button
				type="button"
				onClick={toggleSidebar}
				className="text-muted-foreground hover:text-foreground hover:bg-accent -mr-1 inline-flex h-9 w-9 items-center justify-center rounded-md transition-colors active:scale-95"
				aria-label={openMobile ? "Close navigation menu" : "Open navigation menu"}
				aria-expanded={openMobile}
			>
				{openMobile ? <X className="h-5 w-5" /> : <Menu className="h-5 w-5" />}
			</button>
		</header>
	);
}
