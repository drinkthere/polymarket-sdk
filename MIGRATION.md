# Polymarket SDK Migration Notes

This SDK is now CLOB V2-only for authenticated trading and order signing.

## Current support

- Market discovery:
  - `polymarket/markets`
  - `polymarket/gamma`
- WebSocket market data:
  - `polymarket/ws`
  - `polymarket/ws/market`
  - `polymarket/ws/rtds`
  - `polymarket/ws/user`
- Authenticated order flow:
  - `polymarket/auth`
  - `polymarket/orders`

## V2 order model

Signed orders now use the V2 EIP-712 payload.

Included in the signed payload:

- `salt`
- `maker`
- `signer`
- `tokenId`
- `makerAmount`
- `takerAmount`
- `side`
- `signatureType`
- `timestamp`
- `metadata`
- `builder`

No longer part of the signed payload:

- `taker`
- `nonce`
- `feeRateBps`

The wire payload may still include `expiration` for order posting, but it is not part of the V2 signed struct.

## Contracts

The authenticated order signer now targets the V2 exchange contracts:

- `exchange_v2 = 0xE111180000d2663C0091e4f400237545B87B996B`
- `neg_risk_exchange_v2 = 0xe2222d279d744050d28e00520010520000310F59`

## Builder attribution

Builder attribution is represented through the signed `builder` field.

The SDK does not use legacy builder auth header patterns in the order signer.

## Compatibility expectation

If you have older code that assumes any of the following on signed orders:

- `taker`
- `nonce`
- `feeRateBps`
- V1 EIP-712 order layout

you must update that code before using this SDK version.

## Project guidance

For application code using this SDK:

- Treat `auth + orders` as V2-only
- Treat `markets + gamma + websocket` modules as unaffected by the order-signing migration unless Polymarket changes those APIs separately
