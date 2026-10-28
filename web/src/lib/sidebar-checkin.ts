export function shouldShowSidebarCheckin(input: { creditsEnabled?: boolean; checkinBonusMicrocredits?: number | null; checkedInToday?: boolean }) {
    return Boolean(input.creditsEnabled && (input.checkinBonusMicrocredits || 0) > 0 && !input.checkedInToday);
}

export function sidebarCheckinTitle(brandName: string) {
    const trimmed = brandName.trim();
    const name = !trimmed || trimmed === "影策" ? "源铭振跃" : trimmed;
    return `${name}加油站`;
}
