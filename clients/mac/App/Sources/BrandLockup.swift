import SwiftUI

/// The app icon beside the wordmark's lettering, drawn in the brand ink because the lettering's own dark teal is unreadable in dark mode.
struct BrandLockup: View {
    var body: some View {
        HStack(spacing: 8) {
            Image("BrandIcon")
                .resizable()
                .frame(width: 56, height: 56)
            Image("Wordmark")
                .resizable()
                .renderingMode(.template)
                .scaledToFit()
                .frame(height: 33)
                .foregroundStyle(Color("BrandInk"))
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Earnest")
        .accessibilityAddTraits(.isHeader)
    }
}
