import React, { useEffect, useRef, useState } from 'react'
import { clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

interface Tab {
    id: string
    label: React.ReactNode
    content: React.ReactNode
}

interface TabsProps {
    tabs: Tab[]
    defaultTab?: string
    className?: string
    headerRight?: React.ReactNode
    activeTab?: string
    onTabChange?: (tabId: string) => void
}

export function Tabs({ tabs, defaultTab, className, headerRight, activeTab: controlledActiveTab, onTabChange }: TabsProps) {
    const [internalActiveTab, setInternalActiveTab] = useState(defaultTab || tabs[0]?.id)
    const activeTab = controlledActiveTab ?? internalActiveTab
    const listRef = useRef<HTMLDivElement>(null)
    const [fadeEnd, setFadeEnd] = useState(false)
    const [fadeStart, setFadeStart] = useState(false)

    // Show a fade on the side that has more tabs, and keep the active tab in view.
    const updateFades = () => {
        const el = listRef.current
        if (!el) return
        setFadeStart(el.scrollLeft > 4)
        setFadeEnd(el.scrollLeft + el.clientWidth < el.scrollWidth - 4)
    }
    useEffect(() => {
        const el = listRef.current
        if (!el) return
        updateFades()
        const observer = new ResizeObserver(updateFades)
        observer.observe(el)
        return () => observer.disconnect()
    }, [tabs.length])
    useEffect(() => {
        const active = listRef.current?.querySelector<HTMLElement>('[aria-selected="true"]')
        active?.scrollIntoView({ inline: 'nearest', block: 'nearest' })
    }, [activeTab])

    const handleTabChange = (tabId: string) => {
        if (controlledActiveTab === undefined) {
            setInternalActiveTab(tabId)
        }
        onTabChange?.(tabId)
    }

    return (
        <div className={twMerge("w-full h-full min-h-0 flex flex-col", className)}>
            <div className="flex items-center justify-between gap-2 border-b border-slate-800 bg-slate-950/50 mb-4 sm:mb-6 shrink-0">
                <div
                    ref={listRef}
                    onScroll={updateFades}
                    role="tablist"
                    className="flex min-w-0 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
                    style={{
                        maskImage: `linear-gradient(to right, ${fadeStart ? 'transparent, #000 32px' : '#000, #000'}, #000 calc(100% - 32px), ${fadeEnd ? 'transparent' : '#000'})`,
                        WebkitMaskImage: `linear-gradient(to right, ${fadeStart ? 'transparent, #000 32px' : '#000, #000'}, #000 calc(100% - 32px), ${fadeEnd ? 'transparent' : '#000'})`,
                    }}
                >
                    {tabs.map((tab) => (
                        <button
                            key={tab.id}
                            role="tab"
                            aria-selected={activeTab === tab.id}
                            onClick={() => handleTabChange(tab.id)}
                            className={clsx(
                                "px-4 py-3 text-sm font-medium transition-colors border-b-2 whitespace-nowrap xl:px-5",
                                activeTab === tab.id
                                    ? "border-blue-500 text-white bg-slate-900"
                                    : "border-transparent text-slate-400 hover:text-slate-200 hover:bg-slate-900/50"
                            )}
                        >
                            {tab.label}
                        </button>
                    ))}
                </div>
                {headerRight ? <div className="shrink-0 px-2">{headerRight}</div> : null}
            </div>
            <div className="w-full flex-1 min-h-0">
                {tabs.map(t => (
                    <div key={t.id} className={twMerge("w-full h-full min-h-0", t.id !== activeTab ? "hidden" : "")}>
                        {t.content}
                    </div>
                ))}
            </div>
        </div>
    )
}
