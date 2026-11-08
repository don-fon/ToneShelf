import { Fragment } from 'react'
import type { RubyPart } from '@music/player-policy'

export function LyricText({ text, ruby }: { text: string; ruby?: RubyPart[] }) {
  return <>{ruby ? ruby.map((part, index) => part.reading ?
    <ruby key={index}>{part.text}<rp>（</rp><rt>{part.reading}</rt><rp>）</rp></ruby> :
    <Fragment key={index}>{part.text}</Fragment>) : text}</>
}
