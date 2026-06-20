# diststore/proto

Protobuf definitions for the over-the-wire envelopes.

The protobuf surface (once generated) covers:

- `advertise.proto` — signed `HaveAdvertisement` envelopes emitted by
  `advertise.Advertiser`.
- `fetch.proto` — `FetchRequest` / `FetchResponse` envelopes consumed
  by `fetch.Fetcher`.

The JSON envelopes defined in `advertise/` and `fetch/` remain the
canonical wire form for nodes that have not yet adopted the protobuf
decoder. Mixed-version fleets should keep JSON decode support enabled
on every node until the protobuf rollout is complete; once enabled,
the decoder accepts either encoding so new and old peers interoperate.
