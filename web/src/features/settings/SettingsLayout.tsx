import { Children, Fragment, type ReactNode } from 'react'
import { ChevronLeftIcon } from 'lucide-react'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { Item, ItemActions, ItemContent, ItemGroup, ItemSeparator, ItemTitle } from '@/components/ui/item'
import { useT } from '@/lib/i18n'

// One kind of setting on the right of the settings column: a large title,
// then its groups. The column beside it already says these are settings, so
// there is no caption over the title. On a phone the section stands alone,
// and its top row leads back to the column.
export function SettingsSection({ title, children }: { title: string; children: ReactNode }) {
  const t = useT()
  return (
    <>
      <header className="flex h-12 flex-none items-center px-3 md:hidden">
        <Button variant="ghost" size="sm" asChild className="-ml-1 gap-1 font-normal text-muted-foreground">
          <Link to="/settings">
            <ChevronLeftIcon />
            {t('settings.title')}
          </Link>
        </Button>
      </header>
      <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-10 md:pt-12">
        <div className="mx-auto flex max-w-160 flex-col gap-8">
          <h1 className="text-[1.75rem] font-bold tracking-[-0.03em] text-foreground">{title}</h1>
          {children}
        </div>
      </div>
    </>
  )
}

// A group of settings: an optional caption over a bordered list of rows
// split by hairlines.
export function SettingsGroup({ title, children }: { title?: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="flex flex-col gap-2">
      {title ? <h2 className="text-xs font-medium text-subtle">{title}</h2> : null}
      <ItemGroup className="overflow-hidden rounded-xl border bg-card">
        {Children.toArray(children).map((child, index) => (
          <Fragment key={index}>
            {index > 0 ? <ItemSeparator /> : null}
            {child}
          </Fragment>
        ))}
      </ItemGroup>
    </section>
  )
}

// One setting: its name on the left, the control on the right, no
// explanation under the name. The name never breaks; when the two do not
// fit side by side the control moves under it, still on the right.
export function SettingRow({ label, children }: { label: ReactNode; children: ReactNode }) {
  return (
    <Item role="listitem" size="sm" className="min-h-14 gap-x-4 gap-y-2 py-3">
      <ItemContent className="flex-none">
        <ItemTitle className="text-[0.8125rem] whitespace-nowrap">{label}</ItemTitle>
      </ItemContent>
      <ItemActions className="ml-auto">{children}</ItemActions>
    </Item>
  )
}
