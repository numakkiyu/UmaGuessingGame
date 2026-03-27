import assert from "node:assert/strict";
import { describe, it } from "node:test";
import {
  assetIdFromApiPath,
  buildImageSourceCandidates,
  buildStaticThumbnailPath,
  inferImageExtension,
} from "../src/lib/assets/resolve-image";

describe("image resolution", () => {
  it("extracts asset ids from api paths", () => {
    assert.equal(assetIdFromApiPath("/api/assets/uokka-thumb"), "uokka-thumb");
    assert.equal(assetIdFromApiPath("/assets/characters/uokka-thumb.png"), null);
  });

  it("builds a deduplicated fallback chain", () => {
    assert.deepEqual(
      buildImageSourceCandidates({
        primarySrc: "/api/assets/uokka-thumb",
        proxySrc: "/api/assets/uokka-thumb",
        remoteSrc: "https://patchwiki.biligame.com/example.png",
      }),
      [
        "/api/assets/uokka-thumb",
        "https://patchwiki.biligame.com/example.png",
        "/assets/characters/avatar-fallback.svg",
      ],
    );
  });

  it("infers cache file extensions from allowed remote sources", () => {
    assert.equal(
      inferImageExtension(
        "https://patchwiki.biligame.com/images/umamusume/thumb/b/bd/example.png/100px-avatar.png",
      ),
      "png",
    );
    assert.equal(buildStaticThumbnailPath("uokka-thumb", "png"), "/assets/characters/thumbnails/uokka-thumb.png");
  });
});

