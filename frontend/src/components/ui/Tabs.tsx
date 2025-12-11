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
}

export function Tabs({ tabs, defaultTab, className }: TabsProps) {
    const [activeTab, setActiveTab] = useState(defaultTab || tabs[0]?.id)

    return (
        <div className={twMerge("w-full", className)}>
            <div className="flex border-b border-slate-800 bg-slate-950/50 mb-6 overflow-x-auto">
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
            <div className="w-full">
                {tabs.find(t => t.id === activeTab)?.content}
            </div>
        </div>
    )
}
