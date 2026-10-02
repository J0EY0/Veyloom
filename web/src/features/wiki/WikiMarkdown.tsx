import { useMemo, type AnchorHTMLAttributes, type ImgHTMLAttributes, type ReactNode } from 'react'
import { Link } from 'react-router'
import { wikiFileUrl, type WikiSpace } from '@/api/wiki'
import { MessageResponse } from '@/components/ai-elements/message'
import { cn } from '@/lib/utils'
import { pageHref, resolveFile, resolvePage } from './links'

export interface WikiMarkdownProps {
  text: string
  // The page the text is on, which a relative link is read from.
  from: string
  space: WikiSpace
  className?: string
}

type AnchorProps = AnchorHTMLAttributes<HTMLAnchorElement> & { children?: ReactNode; node?: unknown }
type ImageProps = ImgHTMLAttributes<HTMLImageElement> & { node?: unknown }

// domProps is what the renderer hands a link, less the syntax tree's node,
// which is not the DOM's.
function domProps(props: AnchorProps): AnchorHTMLAttributes<HTMLAnchorElement> {
  const out: AnchorProps = { ...props }
  delete out.node
  return out
}

// A page's markdown, drawn as agents' text is (Streamdown) but read as a
// document (prose-wiki): its tables and code blocks whole, not capped to a
// height to scroll inside. Its links to other pages of the wiki stay in the
// Wiki tab. The hub sends those from the wiki's root; a link out of the
// wiki opens in a new tab, and a footnote stays an anchor on the page. A
// file the project's wiki keeps beside its pages is read from the hub: a
// picture shown in place, anything else opened or downloaded as the hub
// serves it.
export function WikiMarkdown({ text, from, space, className }: WikiMarkdownProps) {
  const components = useMemo(() => {
    const fileUrl = (href: string) => {
      const file = space.kind === 'project' ? resolveFile(from, href) : undefined
      return file && space.kind === 'project' ? wikiFileUrl(space.projectId, file) : undefined
    }
    return {
      a: (props: AnchorProps) => {
        const { href = '', children } = props
        const page = resolvePage(from, href)
        if (page) {
          return (
            <Link to={pageHref(space, page)} className="font-medium text-foreground underline decoration-border underline-offset-3 hover:decoration-foreground">
              {children}
            </Link>
          )
        }
        if (href.startsWith('#')) return <a {...domProps(props)} />
        const file = fileUrl(href)
        return <a {...domProps(props)} href={file ?? href} target="_blank" rel="noreferrer noopener" />
      },
      img: (props: ImageProps) => {
        const out: ImageProps = { ...props }
        delete out.node
        const src = typeof props.src === 'string' ? fileUrl(props.src) : undefined
        return <img {...out} src={src ?? props.src} alt={props.alt ?? ''} className={cn('max-w-full rounded-md', props.className)} />
      },
    }
  }, [from, space])
  return (
    <MessageResponse
      mode="static"
      components={components}
      controls={false}
      lineNumbers={false}
      tableMaxHeight={0}
      codeBlockMaxHeight={0}
      className={cn('prose-agent prose-wiki', className)}
    >
      {text}
    </MessageResponse>
  )
}
