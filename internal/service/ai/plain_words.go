package ai

// plainWordsUserRule is appended to prompts that write sentences a person reads.
// It does not choose pronouns. Each prompt keeps its own voice.
//
// The technical-copy filter PR (#82) is prompt v31 and the warm-web PR (#83)
// is v32. This rule ships as v33 from main (currently v30). After those two
// merge, keep CoachDailyPromptVersion at 33.
const plainWordsUserRule = `

## Lời thường (BẮT BUỘC — mọi câu người dùng đọc)
Viết để người mới hiểu ngay, không cần tra từ.
- Không viết trần các từ này: hàng rào da, hạ kích, barrier, ceramides, niacinamide, BHA, retinoid, kích ứng ngược, purging.
- «ít làm mỏng hàng rào da» → «ít làm da khô rát, dễ bị kích ứng».
- Tên thành phần chỉ được xuất hiện cùng một ý nó làm gì bằng lời thường. Ví dụ: «ceramide — chất giữ ẩm, giúp da đỡ khô rát».
- Không viết hàng rào da. Nói da khô rát, da dễ bị kích ứng, hoặc lớp ngoài da đang yếu.
`
