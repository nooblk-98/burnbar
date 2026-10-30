# Repository social preview

The social card is [assets/social-preview.png](../assets/social-preview.png).
It is 1280 by 640 pixels and under 1 MB. It is separate from the animated
README banner.

## Apply it on GitHub

1. Download the PNG from this repository, or use the local file from your checkout.
2. Open [repository settings](https://github.com/nooblk-98/burnbar/settings).
3. Under **Social preview**, select **Edit > Upload an image** and choose the PNG.

Adding the asset to Git does not apply that setting automatically. See
[GitHub's social preview guide](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/customizing-your-repositorys-social-media-preview).

## Design source

The image was made with built-in image generation, using `assets/icon.png`,
`docs/images/widget-dark.png`, and `docs/images/popup-dark.png` as references.
The finished image was resized to GitHub's recommended dimensions. All displayed
readings are demo data.

Generation prompt:

> Create a finished GitHub repository social preview card for BurnBar, an unofficial Claude and Codex subscription usage monitor for Windows. Wide 2:1 canvas, ideally exactly 1280x640 pixels. Use the supplied images as brand and product references: image 1 is the existing BurnBar logo (preserve its dark rounded square, orange upper meter and green lower meter exactly); image 2 is its real compact taskbar widget; image 3 shows real demo usage panels. Elegant polished developer utility identity, dark charcoal background, warm orange and mint green accents drawn from the logo, generous margins and readable typography at thumbnail size. Layout: small existing logo beside a large crisp white 'BurnBar' wordmark at upper left; below it the exact headline 'Claude + Codex, on your taskbar.' and smaller exact line 'Quotas. History. Alerts.' On the right show a restrained floating product composition based on the supplied actual demo usage panels and compact widget, with softly rounded charcoal cards, orange and green progress bars and fine graph lines. Preserve the supplied UI's overall look; only demo data, no personal information. Footer exact text 'github.com/nooblk-98/burnbar'. Keep all text comfortably inside a 60-pixel safe margin and keep the left half uncluttered. No additional provider logos, no fake supported integrations, no Windows desktop wallpaper, no watermarks, no tiny extra paragraphs, no endorsement claims. This is a new marketing image using the references, not a modification of the existing README GIF.
