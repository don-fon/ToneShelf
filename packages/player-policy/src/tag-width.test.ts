import { expect, it } from 'vitest'
import { tagWidthUnits } from './tag-width'
import cases from '../../../api/tag-width-cases.json'

it('matches the shared fullwidth, halfwidth, combining and emoji cases', () => {
  for (const { name, units } of cases) expect(tagWidthUnits(name), name).toBe(units)
})
