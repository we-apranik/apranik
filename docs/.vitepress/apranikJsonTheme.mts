import oneDarkPro from "shiki/themes/one-dark-pro.mjs";

// One Dark Pro, with JSON in the site's own colours: keys blue, values green.
const keyBlue = "#7aa2ff";
const valueGreen = "#3ccf9f";

export const apranikTheme = {
  ...oneDarkPro,
  name: "apranik-dark",
  tokenColors: [
    ...oneDarkPro.tokenColors,
    {
      scope: ["support.type.property-name.json", "punctuation.support.type.property-name.begin.json", "punctuation.support.type.property-name.end.json"],
      settings: { foreground: keyBlue },
    },
    {
      scope: ["string.quoted.double.json", "punctuation.definition.string.begin.json", "punctuation.definition.string.end.json", "constant.numeric.json", "constant.language.json"],
      settings: { foreground: valueGreen },
    },
  ],
};
