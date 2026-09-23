import {
  BoxIcon,
  FileTextIcon,
  GavelIcon,
  InfoIcon,
  ListChecksIcon,
  MessagesSquareIcon,
  ShapesIcon,
  TriangleAlertIcon,
  WandSparklesIcon,
  type LucideIcon,
  type LucideProps,
} from 'lucide-react'

// The icon a page of each type wears on the relation graph (docs/design.md
// 5.17). The graph is drawn in one colour, so the icon is what tells a
// decision from a pitfall at a glance.
const icons: Record<string, LucideIcon> = {
  Decision: GavelIcon,
  Convention: ListChecksIcon,
  Fact: InfoIcon,
  Pitfall: TriangleAlertIcon,
  Module: BoxIcon,
  Topic: MessagesSquareIcon,
  Pattern: ShapesIcon,
  Skill: WandSparklesIcon,
}

export function TypeIcon({ type, ...props }: LucideProps & { type: string }) {
  const Icon = icons[type] ?? FileTextIcon
  return <Icon {...props} />
}
