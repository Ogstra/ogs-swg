import React, { useState } from 'react'
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
}

export function Tabs({ tabs, defaultTab, className, headerRight }: TabsProps) {
    const [activeTab, setActiveTab] = useState(defaultTab || tabs[0]?.id)

    return (
        <div className={twMerge("w-full h-full min-h-0 flex flex-col", className)}>
            <div className="flex items-center justify-between gap-2 border-b border-slate-800 bg-slate-950/50 mb-4 sm:mb-6 shrink-0">
                <div className="flex overflow-x-auto min-w-0">
                    {tabs.map((tab) => (
                        <button
                            key={tab.id}
                            onClick={() => setActiveTab(tab.id)}
                            className={clsx(
                                "px-6 py-3 text-sm font-medium transition-colors border-b-2 whitespace-nowrap",
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
                {tabs.find(t => t.id === activeTab)?.content}
            </div>
        </div>
    )
}
