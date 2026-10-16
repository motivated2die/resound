# Icons

[Material Symbols](https://github.com/google/material-design-icons) (Outlined, weight 400, grade 0, 24 px) by Google, Apache-2.0 (`LICENSE`). Unmodified SVG files from the repository's `symbols/web/<name>/materialsymbolsoutlined/<name>_24px.svg`; `favorite-fill.svg` is `favorite_fill1_24px.svg`.

The one exception: `discord.svg` is Font Awesome Free 6.7.2's `brands/discord.svg` (CC BY 4.0, attribution in the comment inside the file), for the landing page's picture of the apps Resound plays into.

They replace the Material Symbols web font from Google Fonts. Use one as `<span class="icon" data-icon="<name>"></span>`, and switch it from JS with `el.dataset.icon = "<name>"`. Each name needs a line in `css/styles.css` (`--icon`). The icon takes the text colour and is `1em` square, so `font-size` sets its size.

To add one: download `symbols/web/<name>/materialsymbolsoutlined/<name>_24px.svg` (`_fill1_24px` for the filled form) from the repository, save it here as `<name>.svg`, and add its CSS line.
