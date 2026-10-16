import type MarkdownIt from "markdown-it";

// Inside <div class="cfg-fields">, each "### field" heading starts a field
// card that holds the heading, its badges and its description, so the page
// can lay a field out as one unit.

const fieldsOpening = /^<div class="cfg-fields">\s*$/;
const closingDiv = /^<\/div>\s*$/;
const badgeParagraph = /^<span class="cfg /;

export function configFieldCards(md: MarkdownIt) {
  md.core.ruler.push("config_field_cards", (state) => {
    const { Token } = state;
    const tokens: typeof state.tokens = [];
    let insideFields = false;
    let insideCard = false;

    const htmlBlock = (content: string) => {
      const token = new Token("html_block", "", 0);
      token.content = content;
      token.block = true;
      return token;
    };
    const closeCard = () => {
      if (insideCard) tokens.push(htmlBlock("</section>\n"));
      insideCard = false;
    };

    state.tokens.forEach((token, index) => {
      const startsHeading = token.type === "heading_open";
      const level = startsHeading ? Number(token.tag.slice(1)) : 0;

      if (token.type === "html_block" && fieldsOpening.test(token.content)) {
        insideFields = true;
      } else if (insideFields && token.type === "html_block" && closingDiv.test(token.content)) {
        closeCard();
        insideFields = false;
      } else if (insideFields && startsHeading && level <= 3) {
        closeCard();
        if (level === 3) {
          tokens.push(htmlBlock('<section class="cfg-field">\n'));
          insideCard = true;
        }
      } else if (insideCard && /^container_/.test(token.type)) {
        closeCard();
      } else if (
        insideCard &&
        token.type === "paragraph_open" &&
        state.tokens[index + 1]?.type === "inline" &&
        badgeParagraph.test(state.tokens[index + 1].content)
      ) {
        token.attrJoin("class", "cfg-badges");
      }
      tokens.push(token);
    });
    state.tokens = tokens;
  });
}
