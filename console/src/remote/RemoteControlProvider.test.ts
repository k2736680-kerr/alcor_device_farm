import { describe, expect, it } from 'vitest'
import { remoteEntryURL } from './RemoteControlProvider'

describe('remoteEntryURL', () => {
  it('keeps standalone remote-control entries on the Device Farm origin', () => {
    expect(remoteEntryURL('/console/remote/ios/ticket/control', false))
      .toBe('/console/remote/ios/ticket/control')
  })

  it('routes embedded iOS remote-control entries through the Alcor same-origin gateway', () => {
    expect(remoteEntryURL('/console/remote/ios/ticket/control', true))
      .toBe('/api/v2/device-farm/remote/ios/ticket/control')
  })

  it('does not rewrite Android STF or unrelated URLs', () => {
    expect(remoteEntryURL('http://stf.example.test/#!/control/device', true))
      .toBe('http://stf.example.test/#!/control/device')
  })
})
