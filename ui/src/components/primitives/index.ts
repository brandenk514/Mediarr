// Design-system primitives (issue #115) — barrel export.
//
// The unified shell (#116) and every later view (#117–#123) import from here.
// Tokens live in src/styles/tokens.css (Tailwind v4 @theme); these components
// are the shared building blocks built on them.
export { Badge, BadgeGroup, type BadgeProps, type BadgeTone } from './badge'
export { ConfirmDialog, Modal, type ConfirmDialogProps, type ModalProps } from './modal'
export { Dropdown, type DropdownOption, type DropdownProps } from './dropdown'
export { PosterCard, posterInitials, type PosterCardProps } from './poster-card'
export { SidebarItem, type SidebarItemProps } from './sidebar-item'
