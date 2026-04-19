import React from 'react'
import { Modal } from './Modal'
import { Button } from './Button'

interface ConfirmModalProps {
    isOpen: boolean
    onClose: () => void
    onConfirm: () => void | Promise<void>
    title: string
    message?: string
    confirmLabel?: string
    cancelLabel?: string
    confirmTone?: 'primary' | 'danger'
    isLoading?: boolean
    children?: React.ReactNode
}

export function ConfirmModal({
    isOpen,
    onClose,
    onConfirm,
    title,
    message,
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    confirmTone = 'danger',
    isLoading = false,
    children,
}: ConfirmModalProps) {
    return (
        <Modal
            isOpen={isOpen}
            onClose={onClose}
            title={title}
            size="sm"
            footer={
                <>
                    <Button variant="ghost" onClick={onClose} disabled={isLoading}>
                        {cancelLabel}
                    </Button>
                    <Button
                        variant={confirmTone === 'danger' ? 'danger' : 'primary'}
                        onClick={onConfirm}
                        isLoading={isLoading}
                        autoFocus
                    >
                        {confirmLabel}
                    </Button>
                </>
            }
        >
            <div className="space-y-3">
                {message && <p className="text-sm text-slate-300">{message}</p>}
                {children}
            </div>
        </Modal>
    )
}
